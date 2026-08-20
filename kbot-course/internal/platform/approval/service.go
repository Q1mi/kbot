// Package approval 管理高风险工具调用的人工审批与运行检查点。
package approval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusExecuting = "executing"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusConsumed  = StatusCompleted
)

type Request struct {
	ID, WorkspaceID, RunID, ToolCallID, ToolVersionID string
	Arguments                                         []byte
	Checkpoint                                        []byte
	Status                                            string
	DecidedBy                                         string
	ExpiresAt                                         time.Time
	LeaseOwner                                        string
	LeaseUntil                                        time.Time
	FencingToken                                      uint64
	Attempts                                          int
	LastError                                         string
	argumentsHash                                     [32]byte
}

type Lease struct {
	Request    Request
	Checkpoint []byte
	Token      uint64
}

type Service struct {
	mu       sync.Mutex
	requests map[string]Request
	sequence atomic.Uint64
	now      func() time.Time
	pool     *pgxpool.Pool
}

func NewService() *Service { return &Service{requests: make(map[string]Request), now: time.Now} }

func NewPostgresService(pool *pgxpool.Pool) *Service {
	service := NewService()
	service.pool = pool
	return service
}

func (s *Service) Create(ctx context.Context, request Request) (*Request, error) {
	if request.WorkspaceID == "" || request.RunID == "" || request.ToolCallID == "" || request.ToolVersionID == "" {
		return nil, fmt.Errorf("workspace, run and pinned tool call are required")
	}
	canonical, err := canonicalJSON(request.Arguments)
	if err != nil {
		return nil, fmt.Errorf("canonicalize arguments: %w", err)
	}
	if request.ExpiresAt.IsZero() {
		request.ExpiresAt = s.now().Add(15 * time.Minute)
	}
	if !request.ExpiresAt.After(s.now()) {
		return nil, fmt.Errorf("approval expiration must be in the future")
	}
	request.ID = fmt.Sprintf("approval-%d", s.sequence.Add(1))
	request.Status = StatusPending
	request.Arguments = canonical
	request.Checkpoint = append([]byte(nil), request.Checkpoint...)
	request.argumentsHash = sha256.Sum256(canonical)
	if s.pool != nil {
		return s.createPostgres(ctx, request)
	}
	s.mu.Lock()
	s.requests[request.ID] = request
	s.mu.Unlock()
	copy := cloneRequest(request)
	return &copy, nil
}

// SaveCheckpoint 在 Eino ADK 完成中断快照后，把框架检查点与审批请求绑定。
func (s *Service) SaveCheckpoint(ctx context.Context, workspaceID, requestID string, checkpoint []byte) error {
	if len(checkpoint) == 0 {
		return fmt.Errorf("approval checkpoint is required")
	}
	if s.pool != nil {
		return s.saveCheckpointPostgres(ctx, workspaceID, requestID, checkpoint)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[requestID]
	if !ok || request.WorkspaceID != workspaceID {
		return fmt.Errorf("approval %s not found", requestID)
	}
	if request.Status != StatusPending {
		return fmt.Errorf("approval checkpoint can only be saved while pending")
	}
	request.Checkpoint = append([]byte(nil), checkpoint...)
	s.requests[requestID] = request
	return nil
}

func (s *Service) Decide(ctx context.Context, workspaceID, requestID, actorID string, approved bool) error {
	if actorID == "" {
		return fmt.Errorf("decision actor is required")
	}
	if s.pool != nil {
		return s.decidePostgres(ctx, workspaceID, requestID, actorID, approved)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[requestID]
	if !ok || request.WorkspaceID != workspaceID {
		return fmt.Errorf("approval %s not found", requestID)
	}
	if request.Status != StatusPending {
		return fmt.Errorf("approval is already %s", request.Status)
	}
	if !request.ExpiresAt.After(s.now()) {
		return fmt.Errorf("approval has expired")
	}
	request.DecidedBy = actorID
	if approved {
		request.Status = StatusApproved
	} else {
		request.Status = StatusRejected
	}
	s.requests[requestID] = request
	return nil
}

// ClaimExecution 通过 lease + fencing token 抢占已批准任务。过期 lease 可由其他 Worker 接管。
func (s *Service) ClaimExecution(ctx context.Context, workspaceID, requestID, runID, toolCallID, toolVersionID, workerID string, arguments []byte, leaseDuration time.Duration) (*Lease, error) {
	if workerID == "" || leaseDuration <= 0 {
		return nil, fmt.Errorf("worker and positive lease duration are required")
	}
	canonical, err := canonicalJSON(arguments)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(canonical)
	if s.pool != nil {
		return s.claimPostgres(ctx, workspaceID, requestID, runID, toolCallID, toolVersionID, workerID, hash, leaseDuration)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[requestID]
	if !ok || request.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("approval %s not found", requestID)
	}
	claimable := request.Status == StatusApproved || (request.Status == StatusExecuting && !request.LeaseUntil.After(s.now()))
	if !claimable {
		return nil, fmt.Errorf("approval status is %s", request.Status)
	}
	if request.RunID != runID || request.ToolCallID != toolCallID || request.ToolVersionID != toolVersionID || request.argumentsHash != hash {
		return nil, fmt.Errorf("approval binding does not match resumed tool call")
	}
	if !request.ExpiresAt.After(s.now()) {
		return nil, fmt.Errorf("approval has expired")
	}
	request.Status = StatusExecuting
	request.LeaseOwner = workerID
	request.LeaseUntil = s.now().Add(leaseDuration)
	request.FencingToken++
	request.Attempts++
	s.requests[requestID] = request
	copy := cloneRequest(request)
	return &Lease{Request: copy, Checkpoint: append([]byte(nil), request.Checkpoint...), Token: request.FencingToken}, nil
}

// Complete 只接受当前 fencing token，旧 Worker 即使恢复也无法覆盖新执行者的结果。
func (s *Service) Complete(ctx context.Context, workspaceID, requestID string, token uint64) error {
	if s.pool != nil {
		return s.completePostgres(ctx, workspaceID, requestID, token)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[requestID]
	if !ok || request.WorkspaceID != workspaceID {
		return fmt.Errorf("approval %s not found", requestID)
	}
	if request.Status != StatusExecuting || request.FencingToken != token {
		return fmt.Errorf("stale approval execution token")
	}
	request.Status = StatusCompleted
	request.LeaseOwner = ""
	request.LeaseUntil = time.Time{}
	s.requests[requestID] = request
	return nil
}

func (s *Service) Fail(ctx context.Context, workspaceID, requestID string, token uint64, executionErr error, maxAttempts int) error {
	if maxAttempts <= 0 {
		return fmt.Errorf("max attempts must be positive")
	}
	if s.pool != nil {
		return s.failPostgres(ctx, workspaceID, requestID, token, executionErr, maxAttempts)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[requestID]
	if !ok || request.WorkspaceID != workspaceID {
		return fmt.Errorf("approval %s not found", requestID)
	}
	if request.Status != StatusExecuting || request.FencingToken != token {
		return fmt.Errorf("stale approval execution token")
	}
	if executionErr != nil {
		request.LastError = executionErr.Error()
	}
	request.LeaseOwner, request.LeaseUntil = "", time.Time{}
	if request.Attempts < maxAttempts {
		request.Status = StatusApproved
	} else {
		request.Status = StatusFailed
	}
	s.requests[requestID] = request
	return nil
}

// Resume 保留给第 16 课 API 兼容层；它只获取执行 lease，不会提前标记完成。
func (s *Service) Resume(ctx context.Context, workspaceID, requestID, runID, toolCallID, toolVersionID string, arguments []byte) ([]byte, error) {
	lease, err := s.ClaimExecution(ctx, workspaceID, requestID, runID, toolCallID, toolVersionID, "legacy-resume", arguments, time.Minute)
	if err != nil {
		return nil, err
	}
	return lease.Checkpoint, nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("arguments must contain one JSON value")
	}
	return json.Marshal(value)
}

func (s *Service) Get(ctx context.Context, workspaceID, requestID string) (*Request, error) {
	if s.pool != nil {
		return s.getPostgres(ctx, workspaceID, requestID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[requestID]
	if !ok || request.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("approval %s not found", requestID)
	}
	copy := cloneRequest(request)
	return &copy, nil
}
func cloneRequest(request Request) Request {
	request.Arguments = append([]byte(nil), request.Arguments...)
	request.Checkpoint = append([]byte(nil), request.Checkpoint...)
	return request
}
