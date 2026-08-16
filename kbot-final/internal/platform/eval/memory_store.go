package eval

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Q1mi/kbot/internal/domain"
)

// MemoryStore 是评估服务的内存存储。
type MemoryStore struct {
	mu       sync.RWMutex
	datasets map[string]*domain.EvalDataset
	cases    map[string][]*domain.EvalCase // datasetID -> cases
	runs     map[string]*domain.EvalRun
	scores   map[string][]*domain.EvalScore // runID -> scores
}

// NewMemoryStore 创建评估内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		datasets: make(map[string]*domain.EvalDataset),
		cases:    make(map[string][]*domain.EvalCase),
		runs:     make(map[string]*domain.EvalRun),
		scores:   make(map[string][]*domain.EvalScore),
	}
}

func (s *MemoryStore) CreateDataset(_ context.Context, d *domain.EvalDataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.datasets[d.ID] = d
	return nil
}

func (s *MemoryStore) GetDataset(_ context.Context, id string) (*domain.EvalDataset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.datasets[id]
	if !ok {
		return nil, fmt.Errorf("dataset not found")
	}
	return d, nil
}

func (s *MemoryStore) ListDatasets(_ context.Context, workspaceID string) ([]*domain.EvalDataset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.EvalDataset
	for _, d := range s.datasets {
		if workspaceID == "" || d.WorkspaceID == workspaceID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) AddCase(_ context.Context, c *domain.EvalCase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cases[c.DatasetID] = append(s.cases[c.DatasetID], c)
	return nil
}

func (s *MemoryStore) ListCases(_ context.Context, datasetID string) ([]*domain.EvalCase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cases[datasetID], nil
}

func (s *MemoryStore) CreateRun(_ context.Context, r *domain.EvalRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}

func (s *MemoryStore) GetRun(_ context.Context, runID string) (*domain.EvalRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok {
		return nil, fmt.Errorf("eval run not found")
	}
	return run, nil
}

func (s *MemoryStore) ListRuns(_ context.Context, datasetID string) ([]*domain.EvalRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.EvalRun, 0)
	for _, run := range s.runs {
		if run.DatasetID == datasetID {
			out = append(out, run)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) AddScores(_ context.Context, scores []*domain.EvalScore) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range scores {
		s.scores[sc.RunID] = append(s.scores[sc.RunID], sc)
	}
	return nil
}

func (s *MemoryStore) ListScores(_ context.Context, runID string) ([]*domain.EvalScore, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*domain.EvalScore(nil), s.scores[runID]...), nil
}
