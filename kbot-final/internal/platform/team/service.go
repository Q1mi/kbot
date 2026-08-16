// Package team 提供多 Agent Team 的不可变版本与环境指针持久化。
// 编排算法(Supervisor / Pipeline)在 internal/runtime/team;本包只管「存团队装配 + 按 env 取快照」。
package team

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/util"
)

// Store 是 Team 的存储接口(memory + postgres 双实现)。
type Store interface {
	CreateTeam(ctx context.Context, t *domain.Team) error
	GetTeam(ctx context.Context, teamID string) (*domain.Team, error)
	ListTeams(ctx context.Context, workspaceID string) ([]*domain.Team, error)

	CreateTeamVersion(ctx context.Context, v *domain.TeamVersion) error
	GetTeamVersion(ctx context.Context, versionID string) (*domain.TeamVersion, error)
	ListTeamVersions(ctx context.Context, teamID string) ([]*domain.TeamVersion, error)
	GetTeamCurrentVersion(ctx context.Context, teamID, env string) (*domain.TeamVersion, error)
	UpsertTeamEnv(ctx context.Context, teamID, env, versionID string) error
}

type teamBundleStore interface {
	CreateTeamWithVersion(context.Context, *domain.Team, *domain.TeamVersion) error
}

type teamVersionBundleStore interface {
	CreateTeamVersionAndBindDev(context.Context, *domain.TeamVersion) error
}

// AgentResolver 在建团队那一刻把成员"agent ID"解析成"当前版本 ID"以 pin 死版本。
type AgentResolver interface {
	GetAgentCurrentVersionID(ctx context.Context, agentID, env string) (string, error)
	ValidateAgentVersion(ctx context.Context, agentID, versionID, workspaceID string) error
}

// Service Team 服务。
type Service struct {
	store  Store
	agents AgentResolver // 可为 nil(纯 store 单测)；nil 则不 pin 成员版本
}

// NewService 创建 Team 服务。agents 可为 nil
// （nil ⇒ 不 pin 成员 agent 版本,成员按运行时当前版本执行,仅用于纯 store 单测）。
func NewService(store Store, agents AgentResolver) *Service {
	return &Service{store: store, agents: agents}
}

// snapshot 是 team_versions.snapshot_json 的结构。
type snapshot struct {
	Members []domain.TeamMember `json:"members"`
}

// CreateTeamRequest 创建团队请求。
type CreateTeamRequest struct {
	WorkspaceID string              `json:"workspace_id"`
	Name        string              `json:"name"`
	Mode        string              `json:"mode"` // supervisor / pipeline
	Members     []domain.TeamMember `json:"members"`
}

type CreateTeamVersionRequest struct {
	Members  []domain.TeamMember `json:"members"`
	AgentEnv string              `json:"agent_env"`
}

type TeamVersionView struct {
	ID           string              `json:"id"`
	TeamID       string              `json:"team_id"`
	Version      int                 `json:"version"`
	Members      []domain.TeamMember `json:"members"`
	Environments []string            `json:"environments"`
	CreatedAt    time.Time           `json:"created_at"`
}

// CreateTeam 建团队 + v1(成员快照)并绑定到 dev 环境。
func (s *Service) CreateTeam(ctx context.Context, req CreateTeamRequest) (*domain.Team, *domain.TeamVersion, error) {
	mode := req.Mode
	if mode == "" {
		mode = "supervisor"
	}
	t := &domain.Team{
		ID:          util.GenerateID(),
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Mode:        mode,
		CreatedAt:   time.Now(),
	}
	// pin 成员 agent 版本:建团队这一刻把每个成员的"agent ID"解析成"当前 dev 版本 ID"写死。
	// 之后某个成员 agent 改配置/发新版,本团队快照(及它跑出来的会话)仍按旧版本执行——
	// 与工具 pin 同一个道理,把不可变快照贯彻到多 Agent 编排(讲义 §14.6)。
	members, err := s.pinMembers(ctx, req.WorkspaceID, req.Members, "dev")
	if err != nil {
		return nil, nil, err
	}

	snap, err := json.Marshal(snapshot{Members: members})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	v := &domain.TeamVersion{
		ID:           util.GenerateID(),
		TeamID:       t.ID,
		Version:      1,
		SnapshotJSON: string(snap),
		CreatedAt:    time.Now(),
	}
	if bundle, ok := s.store.(teamBundleStore); ok {
		if err := bundle.CreateTeamWithVersion(ctx, t, v); err != nil {
			return nil, nil, fmt.Errorf("create team and version: %w", err)
		}
		return t, v, nil
	}
	if err := s.store.CreateTeam(ctx, t); err != nil {
		return nil, nil, fmt.Errorf("create team: %w", err)
	}
	if err := s.store.CreateTeamVersion(ctx, v); err != nil {
		return nil, nil, fmt.Errorf("create team version: %w", err)
	}
	if err := s.store.UpsertTeamEnv(ctx, t.ID, "dev", v.ID); err != nil {
		return nil, nil, fmt.Errorf("bind team dev env: %w", err)
	}
	return t, v, nil
}

// CreateTeamVersion 保存新的成员快照并自动更新 dev 环境指针。
func (s *Service) CreateTeamVersion(ctx context.Context, teamID, workspaceID string, req CreateTeamVersionRequest) (*TeamVersionView, error) {
	t, err := s.store.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if t.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("team belongs to another workspace")
	}
	if len(req.Members) == 0 {
		return nil, fmt.Errorf("team members are required")
	}
	members, err := s.pinMembers(ctx, t.WorkspaceID, req.Members, req.AgentEnv)
	if err != nil {
		return nil, err
	}
	versions, err := s.store.ListTeamVersions(ctx, teamID)
	if err != nil {
		return nil, err
	}
	next := 1
	if len(versions) > 0 {
		next = versions[0].Version + 1
	}
	snap, err := json.Marshal(snapshot{Members: members})
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	v := &domain.TeamVersion{ID: util.GenerateID(), TeamID: teamID, Version: next, SnapshotJSON: string(snap), CreatedAt: time.Now()}
	if bundle, ok := s.store.(teamVersionBundleStore); ok {
		if err := bundle.CreateTeamVersionAndBindDev(ctx, v); err != nil {
			return nil, fmt.Errorf("create team version and bind dev: %w", err)
		}
	} else {
		if err := s.store.CreateTeamVersion(ctx, v); err != nil {
			return nil, fmt.Errorf("create team version: %w", err)
		}
		if err := s.store.UpsertTeamEnv(ctx, teamID, "dev", v.ID); err != nil {
			return nil, fmt.Errorf("bind team dev env: %w", err)
		}
	}
	view := teamVersionView(v, members)
	view.Environments = []string{"dev"}
	return view, nil
}

func (s *Service) ListTeamVersions(ctx context.Context, teamID, workspaceID string) ([]*TeamVersionView, error) {
	t, err := s.store.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if t.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("team belongs to another workspace")
	}
	versions, err := s.store.ListTeamVersions(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]*TeamVersionView, 0, len(versions))
	for _, version := range versions {
		var snap snapshot
		if err := json.Unmarshal([]byte(version.SnapshotJSON), &snap); err != nil {
			return nil, fmt.Errorf("unmarshal team version %s: %w", version.ID, err)
		}
		out = append(out, teamVersionView(version, snap.Members))
	}
	for _, env := range []string{"dev", "staging", "prod"} {
		current, currentErr := s.store.GetTeamCurrentVersion(ctx, teamID, env)
		if currentErr != nil {
			continue
		}
		for _, view := range out {
			if view.ID == current.ID {
				view.Environments = append(view.Environments, env)
				break
			}
		}
	}
	return out, nil
}

func (s *Service) PromoteTeamVersion(ctx context.Context, teamID, workspaceID, env, versionID string) error {
	if env != "dev" && env != "staging" && env != "prod" {
		return fmt.Errorf("env must be dev, staging or prod")
	}
	t, err := s.store.GetTeam(ctx, teamID)
	if err != nil {
		return err
	}
	if t.WorkspaceID != workspaceID {
		return fmt.Errorf("team belongs to another workspace")
	}
	v, err := s.store.GetTeamVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.TeamID != teamID {
		return fmt.Errorf("team version belongs to another team")
	}
	return s.store.UpsertTeamEnv(ctx, teamID, env, versionID)
}

func (s *Service) pinMembers(ctx context.Context, workspaceID string, input []domain.TeamMember, env string) ([]domain.TeamMember, error) {
	if env == "" {
		env = "dev"
	}
	members := append([]domain.TeamMember(nil), input...)
	if s.agents == nil {
		return members, nil
	}
	for i := range members {
		if members[i].AgentID == "" {
			return nil, fmt.Errorf("team member agent_id is required")
		}
		if err := s.agents.ValidateAgentVersion(
			ctx, members[i].AgentID, members[i].AgentVersionID, workspaceID,
		); err != nil {
			return nil, fmt.Errorf("validate team member %s: %w", members[i].AgentID, err)
		}
		if members[i].AgentVersionID != "" {
			continue
		}
		vid, err := s.agents.GetAgentCurrentVersionID(ctx, members[i].AgentID, env)
		if err != nil {
			return nil, fmt.Errorf("pin team member %s: %w", members[i].AgentID, err)
		}
		members[i].AgentVersionID = vid
	}
	return members, nil
}

func teamVersionView(v *domain.TeamVersion, members []domain.TeamMember) *TeamVersionView {
	return &TeamVersionView{ID: v.ID, TeamID: v.TeamID, Version: v.Version, Members: members, CreatedAt: v.CreatedAt}
}

// ListTeams 列出工作空间下的团队。
func (s *Service) ListTeams(ctx context.Context, workspaceID string) ([]*domain.Team, error) {
	return s.store.ListTeams(ctx, workspaceID)
}

// RunSpec 是一次团队运行需要的配置(从 env 当前版本解析)。
type RunSpec struct {
	Mode    string
	Members []domain.TeamMember
}

// GetRunSpec 按 team_id + env 取当前版本快照,解析出 mode + members(供 Runtime 装配 Supervisor/Pipeline)。
func (s *Service) GetRunSpec(ctx context.Context, teamID, workspaceID, env string) (*RunSpec, error) {
	t, err := s.store.GetTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("get team: %w", err)
	}
	if workspaceID == "" || t.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("team not found")
	}
	v, err := s.store.GetTeamCurrentVersion(ctx, teamID, env)
	if err != nil {
		return nil, fmt.Errorf("get current team version(env=%s): %w", env, err)
	}
	var snap snapshot
	if v.SnapshotJSON != "" {
		if err := json.Unmarshal([]byte(v.SnapshotJSON), &snap); err != nil {
			return nil, fmt.Errorf("unmarshal snapshot: %w", err)
		}
	}
	return &RunSpec{Mode: t.Mode, Members: snap.Members}, nil
}

// ValidateAgentMember 校验 ad-hoc Team 成员归属。
func (s *Service) ValidateAgentMember(ctx context.Context, workspaceID, agentID, versionID string) error {
	if s.agents == nil {
		return fmt.Errorf("agent resolver is not configured")
	}
	return s.agents.ValidateAgentVersion(ctx, agentID, versionID, workspaceID)
}
