// Package team stores immutable multi-Agent assembly snapshots and environment
// promotions. Orchestration remains in runtime/team.
package team

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type AgentResolver interface {
	ResolveVersion(ctx context.Context, workspaceID, agentID, environment string) (string, error)
}

type Member struct {
	AgentID        string `json:"agent_id"`
	AgentVersionID string `json:"agent_version_id,omitempty"`
	Role           string `json:"role"`
}

type Team struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Mode        string    `json:"mode"`
	CreatedAt   time.Time `json:"created_at"`
}

type Version struct {
	ID           string    `json:"id"`
	TeamID       string    `json:"team_id"`
	Version      int       `json:"version"`
	Members      []Member  `json:"members"`
	Environments []string  `json:"environments"`
	CreatedAt    time.Time `json:"created_at"`
}

type RunSpec struct {
	Mode    string
	Members []Member
}

type Service struct {
	mu         sync.RWMutex
	resolver   AgentResolver
	teams      map[string]Team
	versions   map[string]Version
	promotions map[string]string
	sequence   atomic.Uint64
	postgres   *postgresStore
}

func NewService(resolver AgentResolver) *Service {
	return &Service{resolver: resolver, teams: make(map[string]Team), versions: make(map[string]Version), promotions: make(map[string]string)}
}

func (s *Service) Create(ctx context.Context, workspaceID, name, mode string, members []Member) (Team, Version, error) {
	if err := validateAssembly(workspaceID, name, mode, members); err != nil {
		return Team{}, Version{}, err
	}
	pinned, err := s.pin(ctx, workspaceID, "dev", members)
	if err != nil {
		return Team{}, Version{}, err
	}
	if s.postgres != nil {
		return s.postgres.create(ctx, workspaceID, name, mode, pinned)
	}
	now := time.Now().UTC()
	team := Team{ID: fmt.Sprintf("team-%d", s.sequence.Add(1)), WorkspaceID: workspaceID, Name: name, Mode: mode, CreatedAt: now}
	version := Version{ID: fmt.Sprintf("team-version-%d", s.sequence.Add(1)), TeamID: team.ID, Version: 1, Members: pinned, Environments: []string{"dev"}, CreatedAt: now}
	s.mu.Lock()
	s.teams[team.ID] = team
	s.versions[version.ID] = version
	s.promotions[promotionKey(team.ID, "dev")] = version.ID
	s.mu.Unlock()
	return team, cloneVersion(version), nil
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Team, error) {
	if s.postgres != nil {
		return s.postgres.list(ctx, workspaceID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Team, 0)
	for _, item := range s.teams {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *Service) CreateVersion(ctx context.Context, workspaceID, teamID, agentEnvironment string, members []Member) (Version, error) {
	if s.postgres != nil {
		if err := s.postgres.validateTeam(ctx, workspaceID, teamID, members); err != nil {
			return Version{}, err
		}
		pinned, err := s.pin(ctx, workspaceID, agentEnvironment, members)
		if err != nil {
			return Version{}, err
		}
		return s.postgres.createVersion(ctx, workspaceID, teamID, pinned)
	}
	s.mu.RLock()
	item, ok := s.teams[teamID]
	current := s.versionsForTeamLocked(teamID)
	s.mu.RUnlock()
	if !ok || item.WorkspaceID != workspaceID {
		return Version{}, fmt.Errorf("team %s not found", teamID)
	}
	if err := validateMembers(item.Mode, members); err != nil {
		return Version{}, err
	}
	pinned, err := s.pin(ctx, workspaceID, agentEnvironment, members)
	if err != nil {
		return Version{}, err
	}
	version := Version{ID: fmt.Sprintf("team-version-%d", s.sequence.Add(1)), TeamID: teamID, Version: len(current) + 1, Members: pinned, Environments: []string{"dev"}, CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.versions[version.ID] = version
	s.promotions[promotionKey(teamID, "dev")] = version.ID
	s.mu.Unlock()
	return cloneVersion(version), nil
}

func (s *Service) ListVersions(ctx context.Context, workspaceID, teamID string) ([]Version, error) {
	if s.postgres != nil {
		return s.postgres.listVersions(ctx, workspaceID, teamID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.teams[teamID]
	if !ok || item.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("team %s not found", teamID)
	}
	result := s.versionsForTeamLocked(teamID)
	for index := range result {
		result[index].Environments = nil
		for _, environment := range []string{"dev", "staging", "prod"} {
			if s.promotions[promotionKey(teamID, environment)] == result[index].ID {
				result[index].Environments = append(result[index].Environments, environment)
			}
		}
	}
	return result, nil
}

func (s *Service) Promote(ctx context.Context, workspaceID, teamID, environment, versionID string) error {
	if environment != "dev" && environment != "staging" && environment != "prod" {
		return fmt.Errorf("environment must be dev, staging or prod")
	}
	if s.postgres != nil {
		return s.postgres.promote(ctx, workspaceID, teamID, environment, versionID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.teams[teamID]
	version, versionOK := s.versions[versionID]
	if !ok || item.WorkspaceID != workspaceID || !versionOK || version.TeamID != teamID {
		return fmt.Errorf("team or version not found")
	}
	s.promotions[promotionKey(teamID, environment)] = versionID
	return nil
}

func (s *Service) RunSpec(ctx context.Context, workspaceID, teamID, environment string) (RunSpec, error) {
	if environment == "" {
		environment = "dev"
	}
	if s.postgres != nil {
		return s.postgres.runSpec(ctx, workspaceID, teamID, environment)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.teams[teamID]
	versionID := s.promotions[promotionKey(teamID, environment)]
	version, versionOK := s.versions[versionID]
	if !ok || item.WorkspaceID != workspaceID || !versionOK {
		return RunSpec{}, fmt.Errorf("team %s has no version in %s", teamID, environment)
	}
	return RunSpec{Mode: item.Mode, Members: append([]Member(nil), version.Members...)}, nil
}

func (s *Service) pin(ctx context.Context, workspaceID, environment string, members []Member) ([]Member, error) {
	if environment == "" {
		environment = "dev"
	}
	if s.resolver == nil {
		return nil, fmt.Errorf("agent resolver is required")
	}
	result := append([]Member(nil), members...)
	for index := range result {
		versionID, err := s.resolver.ResolveVersion(ctx, workspaceID, result[index].AgentID, environment)
		if err != nil {
			return nil, fmt.Errorf("pin member %s: %w", result[index].AgentID, err)
		}
		if strings.TrimSpace(versionID) == "" {
			return nil, fmt.Errorf("pin member %s: promoted version is empty", result[index].AgentID)
		}
		result[index].AgentVersionID = versionID
	}
	return result, nil
}

func validateAssembly(workspaceID, name, mode string, members []Member) error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("workspace and team name are required")
	}
	return validateMembers(mode, members)
}

func validateMembers(mode string, members []Member) error {
	if mode != "pipeline" && mode != "supervisor" {
		return fmt.Errorf("mode must be supervisor or pipeline")
	}
	if len(members) == 0 || (mode == "supervisor" && len(members) < 2) {
		return fmt.Errorf("pipeline needs one member; supervisor needs a leader and worker")
	}
	roles := make(map[string]struct{}, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.AgentID) == "" || strings.TrimSpace(member.Role) == "" {
			return fmt.Errorf("member agent_id and role are required")
		}
		if _, exists := roles[member.Role]; exists {
			return fmt.Errorf("duplicate member role %q", member.Role)
		}
		roles[member.Role] = struct{}{}
	}
	return nil
}

func (s *Service) versionsForTeamLocked(teamID string) []Version {
	result := make([]Version, 0)
	for _, version := range s.versions {
		if version.TeamID == teamID {
			result = append(result, cloneVersion(version))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result
}

func promotionKey(teamID, environment string) string { return teamID + ":" + environment }

func cloneVersion(version Version) Version {
	version.Members = append([]Member(nil), version.Members...)
	version.Environments = append([]string(nil), version.Environments...)
	return version
}
