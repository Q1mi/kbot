package team

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Q1mi/kbot/internal/domain"
)

// MemoryStore Team 内存存储。
type MemoryStore struct {
	mu       sync.RWMutex
	teams    map[string]*domain.Team
	versions map[string]*domain.TeamVersion
	envs     map[string]map[string]string // teamID -> env -> versionID
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		teams:    map[string]*domain.Team{},
		versions: map[string]*domain.TeamVersion{},
		envs:     map[string]map[string]string{},
	}
}

func (s *MemoryStore) CreateTeam(_ context.Context, t *domain.Team) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teams[t.ID] = t
	return nil
}

func (s *MemoryStore) CreateTeamWithVersion(_ context.Context, t *domain.Team, v *domain.TeamVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teams[t.ID] = t
	s.versions[v.ID] = v
	s.envs[t.ID] = map[string]string{"dev": v.ID}
	return nil
}

func (s *MemoryStore) CreateTeamVersionAndBindDev(_ context.Context, v *domain.TeamVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[v.ID] = v
	if s.envs[v.TeamID] == nil {
		s.envs[v.TeamID] = map[string]string{}
	}
	s.envs[v.TeamID]["dev"] = v.ID
	return nil
}

func (s *MemoryStore) GetTeam(_ context.Context, teamID string) (*domain.Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.teams[teamID]
	if !ok {
		return nil, fmt.Errorf("team not found")
	}
	return t, nil
}

func (s *MemoryStore) ListTeams(_ context.Context, workspaceID string) ([]*domain.Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Team
	for _, t := range s.teams {
		if workspaceID == "" || t.WorkspaceID == workspaceID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) CreateTeamVersion(_ context.Context, v *domain.TeamVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[v.ID] = v
	return nil
}

func (s *MemoryStore) GetTeamVersion(_ context.Context, versionID string) (*domain.TeamVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[versionID]
	if !ok {
		return nil, fmt.Errorf("team version not found")
	}
	return v, nil
}

func (s *MemoryStore) ListTeamVersions(_ context.Context, teamID string) ([]*domain.TeamVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.TeamVersion
	for _, version := range s.versions {
		if version.TeamID == teamID {
			out = append(out, version)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (s *MemoryStore) GetTeamCurrentVersion(_ context.Context, teamID, env string) (*domain.TeamVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	envs, ok := s.envs[teamID]
	if !ok {
		return nil, fmt.Errorf("team not found")
	}
	vid, ok := envs[env]
	if !ok {
		return nil, fmt.Errorf("no version for env %s", env)
	}
	v, ok := s.versions[vid]
	if !ok {
		return nil, fmt.Errorf("team version not found")
	}
	return v, nil
}

func (s *MemoryStore) UpsertTeamEnv(_ context.Context, teamID, env, versionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.envs[teamID] == nil {
		s.envs[teamID] = map[string]string{}
	}
	s.envs[teamID][env] = versionID
	return nil
}
