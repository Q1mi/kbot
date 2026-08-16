package guardconfig

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type MemoryStore struct {
	mu     sync.RWMutex
	rules  map[string]*Rule
	quotas map[string]*Quota
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rules: map[string]*Rule{}, quotas: map[string]*Quota{}}
}

func (s *MemoryStore) CreateRule(_ context.Context, rule *Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *rule
	s.rules[rule.ID] = &copy
	return nil
}

func (s *MemoryStore) GetRule(_ context.Context, id string) (*Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.rules[id]
	if !ok {
		return nil, fmt.Errorf("guard rule not found")
	}
	copy := *rule
	return &copy, nil
}

func (s *MemoryStore) ListRules(_ context.Context, workspaceID string) ([]*Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Rule, 0)
	for _, rule := range s.rules {
		if rule.WorkspaceID == workspaceID {
			copy := *rule
			out = append(out, &copy)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) UpdateRule(_ context.Context, rule *Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[rule.ID]; !ok {
		return fmt.Errorf("guard rule not found")
	}
	copy := *rule
	s.rules[rule.ID] = &copy
	return nil
}

func (s *MemoryStore) SetQuota(_ context.Context, quota *Quota) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := quota.Dimension + "|" + quota.Period
	if current := s.quotas[key]; current != nil {
		current.Limit = quota.Limit
		quota.Used = current.Used
		return nil
	}
	copy := *quota
	s.quotas[key] = &copy
	return nil
}

func (s *MemoryStore) ListQuotas(_ context.Context, workspaceID, period string) ([]*Quota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := "workspace:" + workspaceID + ":"
	out := make([]*Quota, 0)
	for _, quota := range s.quotas {
		if strings.HasPrefix(quota.Dimension, prefix) && quota.Period == period {
			copy := *quota
			out = append(out, &copy)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dimension < out[j].Dimension })
	return out, nil
}

func (s *MemoryStore) ConsumeQuota(
	_ context.Context, dimension, period string, amount int64,
) (*Quota, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	quota := s.quotas[dimension+"|"+period]
	if quota == nil {
		return nil, true, nil
	}
	if quota.Limit > 0 && quota.Used+amount > quota.Limit {
		copy := *quota
		return &copy, false, nil
	}
	quota.Used += amount
	copy := *quota
	return &copy, true, nil
}

var _ Store = (*MemoryStore)(nil)
