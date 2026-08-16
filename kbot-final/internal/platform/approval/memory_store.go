package approval

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStore 是审批/checkpoint 的内存实现(单测 / 无 DB)。
type MemoryStore struct {
	mu          sync.RWMutex
	approvals   map[string]*Approval
	checkpoints map[string]memoryCheckpoint // approvalID -> 与审批一一对应的快照
}

type memoryCheckpoint struct {
	conversationID string
	state          []byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{approvals: map[string]*Approval{}, checkpoints: map[string]memoryCheckpoint{}}
}

func (s *MemoryStore) CreatePending(_ context.Context, a *Approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *a
	if cp.Status == "" {
		cp.Status = StatusPending
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	if cp.ExecutionStatus == "" {
		cp.ExecutionStatus = "not_started"
	}
	s.approvals[cp.ID] = &cp
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.approvals[id]
	if !ok {
		return nil, fmt.Errorf("approval not found")
	}
	cp := *a
	return &cp, nil
}

func (s *MemoryStore) ListPending(_ context.Context, workspaceID string) ([]*Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Approval
	for _, a := range s.approvals {
		if a.Status == StatusPending && (workspaceID == "" || a.WorkspaceID == workspaceID) {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) ListByConversation(_ context.Context, conversationID string) ([]*Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Approval, 0)
	for _, a := range s.approvals {
		if a.ConversationID == conversationID {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) ResolvePending(_ context.Context, id, workspaceID, status, approverID string) (*Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok {
		return nil, fmt.Errorf("approval not found")
	}
	if a.Status != StatusPending || (workspaceID != "" && a.WorkspaceID != workspaceID) {
		return nil, ErrAlreadyResolved
	}
	a.Status = status
	a.ApproverID = approverID
	now := time.Now()
	a.ResolvedAt = &now
	cp := *a
	return &cp, nil
}

func (s *MemoryStore) BeginExecution(_ context.Context, id, conversationID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	now := time.Now()
	reclaimable := ok && (a.ExecutionStatus == "not_started" || a.ExecutionStatus == "failed" ||
		(a.ExecutionStatus == "executing" && a.ExecutionLeaseUntil != nil && a.ExecutionLeaseUntil.Before(now)))
	if !reclaimable || a.ConversationID != conversationID || a.Status != StatusApproved || a.ExecutionAttempts >= 5 {
		return "", ErrExecutionUnavailable
	}
	token := fmt.Sprintf("lease-%d", now.UnixNano())
	leaseUntil := now.Add(2 * time.Minute)
	a.ExecutionStatus = "executing"
	a.ExecutionStartedAt = &now
	a.ExecutionCompletedAt = nil
	a.ExecutionError = ""
	a.ExecutionAttempts++
	a.ExecutionToken = token
	a.ExecutionLeaseUntil = &leaseUntil
	return token, nil
}

func (s *MemoryStore) RenewExecution(_ context.Context, id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok || a.ExecutionStatus != "executing" || a.ExecutionToken != token {
		return ErrExecutionUnavailable
	}
	leaseUntil := time.Now().Add(2 * time.Minute)
	a.ExecutionLeaseUntil = &leaseUntil
	return nil
}

func (s *MemoryStore) CompleteExecution(_ context.Context, id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok || a.ExecutionStatus != "executing" || a.ExecutionToken != token {
		return ErrExecutionUnavailable
	}
	now := time.Now()
	a.ExecutionStatus = "completed"
	a.ExecutionCompletedAt = &now
	a.ExecutionLeaseUntil = nil
	a.ExecutionToken = ""
	return nil
}

func (s *MemoryStore) FailExecution(_ context.Context, id, token, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok || a.ExecutionStatus != "executing" || a.ExecutionToken != token {
		return ErrExecutionUnavailable
	}
	now := time.Now()
	a.ExecutionStatus = "failed"
	a.ExecutionCompletedAt = &now
	a.ExecutionError = message
	a.ExecutionLeaseUntil = nil
	a.ExecutionToken = ""
	return nil
}

func (s *MemoryStore) ListReadyResumes(_ context.Context, limit int32) ([]*Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Approval
	for _, a := range s.approvals {
		reclaimable := a.ExecutionStatus == "not_started" || a.ExecutionStatus == "failed" ||
			(a.ExecutionStatus == "executing" && a.ExecutionLeaseUntil != nil && a.ExecutionLeaseUntil.Before(time.Now()))
		if a.Status == StatusApproved && a.ExecutionAttempts < 5 && reclaimable {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > int(limit) {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryStore) SaveCheckpoint(_ context.Context, approvalID, conversationID string, state []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if approvalID == "" || conversationID == "" {
		return fmt.Errorf("approval id and conversation id are required")
	}
	if _, exists := s.checkpoints[approvalID]; exists {
		return fmt.Errorf("checkpoint already exists for approval")
	}
	b := make([]byte, len(state))
	copy(b, state)
	s.checkpoints[approvalID] = memoryCheckpoint{conversationID: conversationID, state: b}
	return nil
}

func (s *MemoryStore) CheckpointForApproval(_ context.Context, approvalID, conversationID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	checkpoint, ok := s.checkpoints[approvalID]
	if !ok || checkpoint.conversationID != conversationID {
		return nil, fmt.Errorf("no checkpoint")
	}
	b := make([]byte, len(checkpoint.state))
	copy(b, checkpoint.state)
	return b, nil
}
