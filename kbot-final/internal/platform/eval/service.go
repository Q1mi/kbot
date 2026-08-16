// Package eval 是评估门禁（设计文档 §4.12 / 讲义 §15.3）：让质量从主观判断变成
// 可重复、可阻断、可回归的工程纪律。
package eval

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/util"
)

// Store 评估存储接口。
type Store interface {
	CreateDataset(ctx context.Context, d *domain.EvalDataset) error
	GetDataset(ctx context.Context, id string) (*domain.EvalDataset, error)
	ListDatasets(ctx context.Context, workspaceID string) ([]*domain.EvalDataset, error)
	AddCase(ctx context.Context, c *domain.EvalCase) error
	ListCases(ctx context.Context, datasetID string) ([]*domain.EvalCase, error)
	CreateRun(ctx context.Context, r *domain.EvalRun) error
	GetRun(ctx context.Context, runID string) (*domain.EvalRun, error)
	ListRuns(ctx context.Context, datasetID string) ([]*domain.EvalRun, error)
	AddScores(ctx context.Context, scores []*domain.EvalScore) error
	ListScores(ctx context.Context, runID string) ([]*domain.EvalScore, error)
}

// Service 评估服务。
type Service struct {
	store Store
}

// NewService 创建评估服务。
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Target 是被评估对象的执行器：给定输入产出实际输出（通常包一个 Agent）。
type Target func(ctx context.Context, input string) (string, error)

// CreateDataset 新建评估集。
func (s *Service) CreateDataset(ctx context.Context, workspaceID, name, targetKind string) (*domain.EvalDataset, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("eval dataset name is required")
	}
	if targetKind == "" {
		targetKind = "agent"
	}
	if targetKind != "agent" {
		return nil, fmt.Errorf("target_kind currently supports agent")
	}
	d := &domain.EvalDataset{
		ID: util.GenerateID(), WorkspaceID: workspaceID, Name: name,
		TargetKind: targetKind, CreatedAt: time.Now(),
	}
	if err := s.store.CreateDataset(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// AddCase 往评估集加用例。
func (s *Service) AddCase(ctx context.Context, datasetID, input, expected, metadata string) (*domain.EvalCase, error) {
	c := &domain.EvalCase{
		ID: util.GenerateID(), DatasetID: datasetID,
		Input: input, Expected: expected, Metadata: metadata, CreatedAt: time.Now(),
	}
	if err := s.store.AddCase(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// AddCaseFromConversation 把一条（被点踩的）对话沉淀为评估用例（§15.3 坏样本回流）。
func (s *Service) AddCaseFromConversation(ctx context.Context, datasetID, conversationID, input, expected string) (*domain.EvalCase, error) {
	return s.AddCase(ctx, datasetID, input, expected, `{"source":"conversation","conversation_id":"`+conversationID+`"}`)
}

// RunRequest 一次评估运行的参数。
type RunRequest struct {
	DatasetID   string
	TargetID    string
	WorkspaceID string
	Judge       Judge
	Threshold   float64
}

// RunHistoryItem 是控制面展示的一次运行及其逐用例分数。
type RunHistoryItem struct {
	Run    *domain.EvalRun     `json:"run"`
	Scores []*domain.EvalScore `json:"scores"`
}

// RunResult 评估运行结果。
type RunResult struct {
	RunID    string              `json:"run_id"`
	PassRate float64             `json:"pass_rate"`
	Passed   bool                `json:"passed"`
	Total    int                 `json:"total"`
	Scores   []*domain.EvalScore `json:"scores"`
}

// Run 跑一次评估：对每个用例执行 target → Judge 打分 → 计算通过率 vs 阈值。
// Judge 并发受限（也走 LLM Gateway 时要受限流约束，§15.3）。
func (s *Service) Run(ctx context.Context, req RunRequest, target Target) (*RunResult, error) {
	if req.Judge == nil {
		return nil, fmt.Errorf("judge is required")
	}
	if target == nil {
		return nil, fmt.Errorf("target is required")
	}
	if req.Threshold < 0 || req.Threshold > 1 {
		return nil, fmt.Errorf("threshold must be between 0 and 1")
	}
	if req.WorkspaceID != "" {
		if err := s.EnsureDatasetWorkspace(ctx, req.DatasetID, req.WorkspaceID); err != nil {
			return nil, err
		}
	}
	cases, err := s.store.ListCases(ctx, req.DatasetID)
	if err != nil {
		return nil, fmt.Errorf("list cases: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("dataset has no cases")
	}

	runID := util.GenerateID()
	scores := make([]*domain.EvalScore, len(cases))

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, c := range cases {
		i, c := i, c
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			actual, err := target(ctx, c.Input)
			if err != nil {
				scores[i] = &domain.EvalScore{RunID: runID, CaseID: c.ID, Dimension: "correctness", Score: 0, Reason: "target error: " + err.Error()}
				return
			}
			jr := req.Judge.Score(ctx, c.Expected, actual)
			scores[i] = &domain.EvalScore{RunID: runID, CaseID: c.ID, Dimension: "correctness", Score: jr.Score, Reason: jr.Reason}
		}()
	}
	wg.Wait()

	// 确定性规则要求完整命中；LLM Judge 使用 0.7 作为单用例通过线。
	casePassScore := 0.999
	if req.Judge.Kind() != "deterministic" {
		casePassScore = 0.7
	}
	pass := 0
	for _, sc := range scores {
		if sc.Score >= casePassScore {
			pass++
		}
	}
	rate := float64(pass) / float64(len(cases))
	passed := rate >= req.Threshold

	run := &domain.EvalRun{
		ID: runID, DatasetID: req.DatasetID, TargetID: req.TargetID,
		JudgeID: req.Judge.Kind(), Status: statusOf(passed), PassRate: rate, Threshold: req.Threshold,
		CreatedAt: time.Now(),
	}
	now := time.Now()
	run.FinishedAt = &now
	if err := s.store.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create eval run: %w", err)
	}
	if err := s.store.AddScores(ctx, scores); err != nil {
		return nil, fmt.Errorf("add eval scores: %w", err)
	}

	return &RunResult{RunID: runID, PassRate: rate, Passed: passed, Total: len(cases), Scores: scores}, nil
}

// ListDatasets 透传。
func (s *Service) ListDatasets(ctx context.Context, ws string) ([]*domain.EvalDataset, error) {
	return s.store.ListDatasets(ctx, ws)
}

// EnsureDatasetWorkspace 校验评估集属于当前工作空间。
func (s *Service) EnsureDatasetWorkspace(ctx context.Context, datasetID, workspaceID string) error {
	dataset, err := s.store.GetDataset(ctx, datasetID)
	if err != nil {
		return err
	}
	if dataset.WorkspaceID != workspaceID {
		return fmt.Errorf("eval dataset not found")
	}
	return nil
}

func (s *Service) ListCases(ctx context.Context, datasetID, workspaceID string) ([]*domain.EvalCase, error) {
	if err := s.EnsureDatasetWorkspace(ctx, datasetID, workspaceID); err != nil {
		return nil, err
	}
	return s.store.ListCases(ctx, datasetID)
}

func (s *Service) ListRunHistory(ctx context.Context, datasetID, workspaceID string) ([]*RunHistoryItem, error) {
	if err := s.EnsureDatasetWorkspace(ctx, datasetID, workspaceID); err != nil {
		return nil, err
	}
	runs, err := s.store.ListRuns(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	out := make([]*RunHistoryItem, 0, len(runs))
	for _, run := range runs {
		scores, err := s.store.ListScores(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, &RunHistoryItem{Run: run, Scores: scores})
	}
	return out, nil
}

func statusOf(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}
