// Package agent 提供 Agent Builder 与配置快照（设计文档 §4.7 / 讲义 §14.6）。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"github.com/Q1mi/kbot/internal/util"
)

// Service Agent服务
type Service struct {
	store  Store
	prompt PromptResolver
	skills SkillResolver
	tools  ToolResolver
	kbs    KBResolver
}

// Store Agent存储接口
type Store interface {
	GetAgent(ctx context.Context, agentID string) (*domain.Agent, error)
	ListAgents(ctx context.Context, workspaceID string) ([]*domain.Agent, error)
	CreateAgent(ctx context.Context, agent *domain.Agent) error
	CreateAgentVersion(ctx context.Context, version *domain.AgentVersion) error
	GetAgentVersion(ctx context.Context, versionID string) (*domain.AgentVersion, error)
	ListAgentVersions(ctx context.Context, agentID string) ([]*domain.AgentVersion, error)
	GetAgentCurrentVersion(ctx context.Context, agentID, env string) (*domain.AgentVersion, error)
	SetAgentEnvBinding(ctx context.Context, agentID, env, versionID string) error
	CreateConversation(ctx context.Context, conversation *domain.Conversation) error
	UpdateConversationRuntimeConfig(ctx context.Context, conversationID, configJSON string) error
	GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error)
	ListConversations(ctx context.Context, workspaceID, userID, agentID string, limit, offset int32) ([]*domain.Conversation, error)
	GetConversationMessages(ctx context.Context, conversationID string) ([]*domain.Message, error)
	CreateMessage(ctx context.Context, message *domain.Message) error
}

type agentBundleStore interface {
	CreateAgentWithVersion(context.Context, *domain.Agent, *domain.AgentVersion) error
}

type conversationTurnStore interface {
	ClaimConversationTurn(context.Context, string, bool) (string, error)
	RenewConversationTurn(context.Context, string, string) error
	CommitConversationTurn(context.Context, string, string, []*domain.Message, string) error
	ReleaseConversationTurn(context.Context, string, string, string) error
}

// PromptResolver 把 pinned 的 Prompt 版本渲染成文本（Prompt 中心）。
type PromptResolver interface {
	RenderByVersion(ctx context.Context, versionID string, vars map[string]any) (string, error)
	GetPrompt(ctx context.Context, promptID string) (*domain.Prompt, error)
	GetVersion(ctx context.Context, versionID string) (*domain.PromptVersion, error)
	ResolveVersion(ctx context.Context, promptID, env, userID string) (string, error)
	ResolveConfig(ctx context.Context, promptID, env, userID string, vars map[string]any) (*prompt.ResolvedConfig, error)
	ResolveConfigByVersion(ctx context.Context, versionID string, vars map[string]any, experimentID, variant string) (*prompt.ResolvedConfig, error)
}

// SkillResolver 把 Skill 版本解析为 Runtime Spec。
type SkillResolver interface {
	GetSpec(ctx context.Context, versionID string) (*skill.Spec, error)
}

// ToolResolver 在建快照那一刻把"工具注册 ID"解析成"当前版本 ID"以 pin 死版本。
type ToolResolver interface {
	GetToolCurrentVersionID(ctx context.Context, toolID string) (string, error)
	GetToolIDByVersion(ctx context.Context, versionID string) (string, error)
	GetToolMeta(ctx context.Context, toolID string) (*domain.Tool, error)
}

type KBResolver interface {
	KBExists(ctx context.Context, workspaceID, kbID string) (bool, error)
}

// NewService 创建Agent服务。prompt/skills/tools 可为 nil
// （nil tools ⇒ 不允许创建带工具的 Agent，仅用于纯字面量/无工具的场景与单测）。
func NewService(store Store, prompt PromptResolver, skills SkillResolver, tools ToolResolver) *Service {
	return &Service{store: store, prompt: prompt, skills: skills, tools: tools}
}

// WithKBResolver 开启 Agent 快照知识库所有权校验。
func (s *Service) WithKBResolver(kbs KBResolver) *Service {
	s.kbs = kbs
	return s
}

// storedSnapshot 是持久化在 agent_versions.snapshot_json 里的快照（写死依赖版本号）。
type storedSnapshot struct {
	ID                    string   `json:"id"`
	SystemPrompt          string   `json:"system_prompt,omitempty"`            // 字面量（简单 Agent）
	SystemPromptID        string   `json:"system_prompt_id,omitempty"`         // Prompt 中心
	SystemPromptVersionID string   `json:"system_prompt_version_id,omitempty"` // pinned 版本
	UserPromptID          string   `json:"user_prompt_id,omitempty"`           // 首轮任务输入模板
	PromptEnv             string   `json:"prompt_env,omitempty"`               // 新会话按环境解析基线/灰度
	ToolIDs               []string `json:"tool_ids,omitempty"`                 // 控制面编辑用注册 ID
	ToolVersionIDs        []string `json:"tool_version_ids,omitempty"`         // pin 死的工具【版本 ID】(非注册 ID)
	SkillVersionIDs       []string `json:"skill_version_ids,omitempty"`
	KBIDs                 []string `json:"kb_ids,omitempty"`
	AllowNetwork          bool     `json:"allow_network,omitempty"`
	NetworkPolicySet      bool     `json:"network_policy_set,omitempty"`
	MaxSteps              int      `json:"max_steps,omitempty"`
}

// CreateAgentRequest 创建Agent请求
type CreateAgentRequest struct {
	WorkspaceID           string   `json:"workspace_id"`
	Name                  string   `json:"name"`
	Template              string   `json:"template"`
	SystemPrompt          string   `json:"system_prompt"`            // 字面量
	SystemPromptVersionID string   `json:"system_prompt_version_id"` // 或绑定 Prompt 中心版本
	SystemPromptID        string   `json:"system_prompt_id"`         // 推荐：绑定 Prompt，创建会话时解析灰度
	UserPromptID          string   `json:"user_prompt_id"`           // 可选：首轮任务输入模板
	PromptEnv             string   `json:"prompt_env"`
	ToolIDs               []string `json:"tool_ids"`
	SkillVersionIDs       []string `json:"skill_version_ids"`
	KBIDs                 []string `json:"kb_ids"`
	AllowNetwork          *bool    `json:"allow_network,omitempty"`
	MaxSteps              int      `json:"max_steps"`
	CreatedBy             string   `json:"created_by"`
}

// AgentVersionConfig 是创建不可变 Agent 版本时提交的可编辑配置。
// ToolIDs 是注册 ID；Service 会在保存快照时解析并固化 ToolVersionIDs。
type AgentVersionConfig struct {
	SystemPrompt          string   `json:"system_prompt"`
	SystemPromptVersionID string   `json:"system_prompt_version_id"`
	SystemPromptID        string   `json:"system_prompt_id"`
	UserPromptID          string   `json:"user_prompt_id"`
	PromptEnv             string   `json:"prompt_env"`
	ToolIDs               []string `json:"tool_ids"`
	SkillVersionIDs       []string `json:"skill_version_ids"`
	KBIDs                 []string `json:"kb_ids"`
	AllowNetwork          *bool    `json:"allow_network,omitempty"`
	MaxSteps              int      `json:"max_steps"`
}

// AgentVersionView 向控制面返回版本元数据与可再次编辑的配置。
type AgentVersionView struct {
	ID           string             `json:"id"`
	AgentID      string             `json:"agent_id"`
	Version      int                `json:"version"`
	Config       AgentVersionConfig `json:"config"`
	Environments []string           `json:"environments"`
	CreatedBy    string             `json:"created_by"`
	CreatedAt    time.Time          `json:"created_at"`
}

// UserPromptInputSpec 描述某个 Agent 环境当前首轮任务模板及其变量契约。
type UserPromptInputSpec struct {
	Enabled         bool   `json:"enabled"`
	PromptID        string `json:"prompt_id,omitempty"`
	PromptName      string `json:"prompt_name,omitempty"`
	PromptEnv       string `json:"prompt_env,omitempty"`
	PromptVersionID string `json:"prompt_version_id,omitempty"`
	PromptVersion   int    `json:"prompt_version,omitempty"`
	VariablesSchema string `json:"variables_schema,omitempty"`
}

// CreateAgent 创建Agent及其 v1 immutable 快照，并绑定到 dev 环境。
func (s *Service) CreateAgent(ctx context.Context, req CreateAgentRequest) (*domain.Agent, error) {
	if err := s.validatePromptBindings(ctx, req.WorkspaceID, AgentVersionConfig{
		SystemPrompt: req.SystemPrompt, SystemPromptID: req.SystemPromptID,
		SystemPromptVersionID: req.SystemPromptVersionID, UserPromptID: req.UserPromptID,
		PromptEnv: req.PromptEnv,
	}); err != nil {
		return nil, err
	}
	ag := &domain.Agent{
		ID:          util.GenerateID(),
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Template:    req.Template,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	snapJSON, err := s.buildSnapshotJSON(ctx, ag.ID, req.WorkspaceID, AgentVersionConfig{
		SystemPrompt: req.SystemPrompt, SystemPromptID: req.SystemPromptID,
		SystemPromptVersionID: req.SystemPromptVersionID, UserPromptID: req.UserPromptID, PromptEnv: req.PromptEnv,
		ToolIDs: req.ToolIDs, SkillVersionIDs: req.SkillVersionIDs, KBIDs: req.KBIDs,
		AllowNetwork: req.AllowNetwork, MaxSteps: req.MaxSteps,
	})
	if err != nil {
		return nil, err
	}
	version := &domain.AgentVersion{
		ID:           util.GenerateID(),
		AgentID:      ag.ID,
		Version:      1,
		SnapshotJSON: string(snapJSON),
		CreatedBy:    req.CreatedBy,
		CreatedAt:    time.Now(),
	}
	if bundle, ok := s.store.(agentBundleStore); ok {
		if err := bundle.CreateAgentWithVersion(ctx, ag, version); err != nil {
			return nil, fmt.Errorf("create agent and version: %w", err)
		}
		return ag, nil
	}
	if err := s.store.CreateAgent(ctx, ag); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	if err := s.store.CreateAgentVersion(ctx, version); err != nil {
		return nil, fmt.Errorf("create agent version: %w", err)
	}
	return ag, nil
}

// CreateAgentVersion 为已有 Agent 创建新版本，并自动成为 dev 环境当前版本。
func (s *Service) CreateAgentVersion(
	ctx context.Context,
	agentID, workspaceID string,
	cfg AgentVersionConfig,
	actor string,
) (*AgentVersionView, error) {
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if workspaceID != "" && ag.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("agent does not belong to current workspace")
	}
	if err := s.validatePromptBindings(ctx, ag.WorkspaceID, cfg); err != nil {
		return nil, err
	}
	snapshotJSON, err := s.buildSnapshotJSON(ctx, agentID, ag.WorkspaceID, cfg)
	if err != nil {
		return nil, err
	}
	versions, err := s.store.ListAgentVersions(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	v := &domain.AgentVersion{
		ID: util.GenerateID(), AgentID: agentID, Version: len(versions) + 1,
		SnapshotJSON: string(snapshotJSON), CreatedBy: actor, CreatedAt: time.Now(),
	}
	if err := s.store.CreateAgentVersion(ctx, v); err != nil {
		return nil, fmt.Errorf("create agent version: %w", err)
	}
	return s.versionView(ctx, v)
}

// ListAgentVersions 返回 Agent 的全部不可变版本，按版本号倒序。
func (s *Service) ListAgentVersions(ctx context.Context, agentID, workspaceID string) ([]*AgentVersionView, error) {
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if workspaceID != "" && ag.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("agent does not belong to current workspace")
	}
	versions, err := s.store.ListAgentVersions(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	out := make([]*AgentVersionView, 0, len(versions))
	for _, v := range versions {
		view, err := s.versionView(ctx, v)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	for _, env := range []string{"dev", "staging", "prod"} {
		current, currentErr := s.store.GetAgentCurrentVersion(ctx, agentID, env)
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

// PromoteAgentVersion 把指定版本绑定到 dev/staging/prod 环境。
func (s *Service) PromoteAgentVersion(ctx context.Context, agentID, workspaceID, env, versionID string) error {
	if env != "dev" && env != "staging" && env != "prod" {
		return fmt.Errorf("env must be dev, staging or prod")
	}
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if workspaceID != "" && ag.WorkspaceID != workspaceID {
		return fmt.Errorf("agent does not belong to current workspace")
	}
	v, err := s.store.GetAgentVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get agent version: %w", err)
	}
	if v.AgentID != agentID {
		return fmt.Errorf("agent version does not belong to agent")
	}
	return s.store.SetAgentEnvBinding(ctx, agentID, env, versionID)
}

// AttachSkillVersion 基于 dev 当前快照创建一个包含指定 Skill 的新版本。
// 该方法让旧 subscribe API 也遵守不可变版本语义，并立即影响后续新会话。
func (s *Service) AttachSkillVersion(
	ctx context.Context,
	agentID, workspaceID, skillVersionID, actor string,
) (*AgentVersionView, error) {
	current, err := s.store.GetAgentCurrentVersion(ctx, agentID, "dev")
	if err != nil {
		return nil, fmt.Errorf("get current agent version: %w", err)
	}
	view, err := s.versionView(ctx, current)
	if err != nil {
		return nil, err
	}
	for _, existing := range view.Config.SkillVersionIDs {
		if existing == skillVersionID {
			return view, nil
		}
	}
	view.Config.SkillVersionIDs = append(view.Config.SkillVersionIDs, skillVersionID)
	return s.CreateAgentVersion(ctx, agentID, workspaceID, view.Config, actor)
}

func (s *Service) buildSnapshotJSON(ctx context.Context, agentID, workspaceID string, cfg AgentVersionConfig) ([]byte, error) {
	cfg.ToolIDs = uniqueStrings(cfg.ToolIDs)
	cfg.SkillVersionIDs = uniqueStrings(cfg.SkillVersionIDs)
	cfg.KBIDs = uniqueStrings(cfg.KBIDs)
	if cfg.PromptEnv == "" {
		cfg.PromptEnv = prompt.EnvDev
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 6
	}
	allowNetwork := true // 兼容旧 API 客户端；新版 Admin 会显式提交 true/false。
	if cfg.AllowNetwork != nil {
		allowNetwork = *cfg.AllowNetwork
	}

	toolVersionIDs := make([]string, 0, len(cfg.ToolIDs))
	toolNames := make(map[string]bool, len(cfg.ToolIDs))
	for _, toolID := range cfg.ToolIDs {
		if s.tools == nil {
			return nil, fmt.Errorf("agent has tools but tool resolver is not configured")
		}
		versionID, err := s.tools.GetToolCurrentVersionID(ctx, toolID)
		if err != nil {
			return nil, fmt.Errorf("pin tool %s: %w", toolID, err)
		}
		meta, err := s.tools.GetToolMeta(ctx, toolID)
		if err != nil {
			return nil, fmt.Errorf("get tool %s: %w", toolID, err)
		}
		if meta.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("tool %s does not belong to current workspace", toolID)
		}
		toolVersionIDs = append(toolVersionIDs, versionID)
		toolNames[meta.Name] = true
	}

	if len(cfg.KBIDs) > 0 && s.kbs == nil {
		return nil, fmt.Errorf("agent has knowledge bases but kb resolver is not configured")
	}
	kbIDs := make(map[string]bool, len(cfg.KBIDs))
	for _, id := range cfg.KBIDs {
		ok, err := s.kbs.KBExists(ctx, workspaceID, id)
		if err != nil {
			return nil, fmt.Errorf("check knowledge base %s: %w", id, err)
		}
		if !ok {
			return nil, fmt.Errorf("knowledge base %s does not belong to current workspace", id)
		}
		kbIDs[id] = true
	}
	if len(cfg.SkillVersionIDs) > 0 && s.skills == nil {
		return nil, fmt.Errorf("agent has skills but skill resolver is not configured")
	}
	for _, versionID := range cfg.SkillVersionIDs {
		sp, err := s.skills.GetSpec(ctx, versionID)
		if err != nil {
			return nil, fmt.Errorf("resolve skill version %s: %w", versionID, err)
		}
		if sp.Status != "published" {
			return nil, fmt.Errorf("skill %s version must be published", sp.Name)
		}
		if sp.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("skill version %s does not belong to current workspace", versionID)
		}
		if sp.RequiresNetwork && !allowNetwork {
			return nil, fmt.Errorf("skill %s requires network access", sp.Name)
		}
		for _, name := range sp.AllowedTools {
			if !toolNames[name] {
				return nil, fmt.Errorf("skill %s requires tool %s to be attached to the agent", sp.Name, name)
			}
		}
		for _, kbID := range sp.AllowedKBs {
			if !kbIDs[kbID] {
				return nil, fmt.Errorf("skill %s requires knowledge base %s to be attached to the agent", sp.Name, kbID)
			}
		}
	}

	snap := storedSnapshot{
		ID: agentID, SystemPrompt: cfg.SystemPrompt, SystemPromptID: cfg.SystemPromptID,
		SystemPromptVersionID: cfg.SystemPromptVersionID, UserPromptID: cfg.UserPromptID, PromptEnv: cfg.PromptEnv,
		ToolIDs: cfg.ToolIDs, ToolVersionIDs: toolVersionIDs,
		SkillVersionIDs: cfg.SkillVersionIDs, KBIDs: cfg.KBIDs,
		AllowNetwork: allowNetwork, NetworkPolicySet: true, MaxSteps: cfg.MaxSteps,
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	return b, nil
}

func (s *Service) versionView(ctx context.Context, v *domain.AgentVersion) (*AgentVersionView, error) {
	var snap storedSnapshot
	if err := json.Unmarshal([]byte(v.SnapshotJSON), &snap); err != nil {
		return nil, fmt.Errorf("unmarshal agent version %s: %w", v.ID, err)
	}
	toolIDs := append([]string(nil), snap.ToolIDs...)
	allowNetwork := snap.AllowNetwork
	if !snap.NetworkPolicySet {
		allowNetwork = true
	}
	if len(toolIDs) == 0 && len(snap.ToolVersionIDs) > 0 && s.tools != nil {
		for _, versionID := range snap.ToolVersionIDs {
			toolID, err := s.tools.GetToolIDByVersion(ctx, versionID)
			if err != nil {
				return nil, fmt.Errorf("resolve tool version %s: %w", versionID, err)
			}
			toolIDs = append(toolIDs, toolID)
		}
	}
	return &AgentVersionView{
		ID: v.ID, AgentID: v.AgentID, Version: v.Version, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt,
		Config: AgentVersionConfig{
			SystemPrompt: snap.SystemPrompt, SystemPromptID: snap.SystemPromptID,
			SystemPromptVersionID: snap.SystemPromptVersionID, UserPromptID: snap.UserPromptID, PromptEnv: snap.PromptEnv,
			ToolIDs: toolIDs, SkillVersionIDs: snap.SkillVersionIDs, KBIDs: snap.KBIDs,
			AllowNetwork: &allowNetwork, MaxSteps: snap.MaxSteps,
		},
	}, nil
}

func (s *Service) validatePromptBindings(ctx context.Context, workspaceID string, cfg AgentVersionConfig) error {
	systemSources := 0
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		systemSources++
	}
	if cfg.SystemPromptID != "" {
		systemSources++
	}
	if cfg.SystemPromptVersionID != "" {
		systemSources++
	}
	if systemSources > 1 {
		return fmt.Errorf("choose exactly one system prompt source: literal, prompt binding, or pinned version")
	}
	if cfg.PromptEnv != "" && cfg.PromptEnv != prompt.EnvDev && cfg.PromptEnv != prompt.EnvStaging && cfg.PromptEnv != prompt.EnvProd {
		return fmt.Errorf("prompt_env must be dev, staging or prod")
	}
	if cfg.SystemPromptID == "" && cfg.SystemPromptVersionID == "" && cfg.UserPromptID == "" {
		return nil
	}
	if s.prompt == nil {
		return fmt.Errorf("prompt resolver is not configured")
	}
	if cfg.SystemPromptID != "" {
		metadata, err := s.prompt.GetPrompt(ctx, cfg.SystemPromptID)
		if err != nil {
			return fmt.Errorf("get system prompt: %w", err)
		}
		if metadata.WorkspaceID != workspaceID {
			return fmt.Errorf("system prompt does not belong to current workspace")
		}
		if isUserPromptCategory(metadata.Category) {
			return fmt.Errorf("system prompt binding cannot reference a user prompt template")
		}
	}
	if cfg.SystemPromptVersionID != "" {
		version, err := s.prompt.GetVersion(ctx, cfg.SystemPromptVersionID)
		if err != nil {
			return fmt.Errorf("get pinned system prompt version: %w", err)
		}
		metadata, err := s.prompt.GetPrompt(ctx, version.PromptID)
		if err != nil {
			return fmt.Errorf("get pinned system prompt: %w", err)
		}
		if metadata.WorkspaceID != workspaceID {
			return fmt.Errorf("pinned system prompt does not belong to current workspace")
		}
		if isUserPromptCategory(metadata.Category) {
			return fmt.Errorf("pinned system prompt cannot reference a user prompt template")
		}
	}
	if cfg.UserPromptID != "" {
		metadata, err := s.prompt.GetPrompt(ctx, cfg.UserPromptID)
		if err != nil {
			return fmt.Errorf("get user prompt template: %w", err)
		}
		if metadata.WorkspaceID != workspaceID {
			return fmt.Errorf("user prompt template does not belong to current workspace")
		}
		if !isUserPromptCategory(metadata.Category) {
			return fmt.Errorf("user prompt binding must reference a user prompt template")
		}
	}
	return nil
}

func isUserPromptCategory(category string) bool {
	return strings.HasSuffix(strings.TrimSpace(category), "-user-template")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// GetAgentSnapshotByVersion 按 pinned 版本号解析快照（实现 engine.Platform）。
func (s *Service) GetAgentSnapshotByVersion(ctx context.Context, agentVersionID string) (*engine.AgentSnapshot, error) {
	version, err := s.store.GetAgentVersion(ctx, agentVersionID)
	if err != nil {
		return nil, fmt.Errorf("get agent version: %w", err)
	}
	var snap storedSnapshot
	if err := json.Unmarshal([]byte(version.SnapshotJSON), &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	allowNetwork := snap.AllowNetwork
	if !snap.NetworkPolicySet {
		allowNetwork = true
	}

	// 解析 system prompt：优先 Prompt 中心 pinned 版本，否则字面量。
	systemPrompt := snap.SystemPrompt
	var promptVersionID, modelProfileVersionID string
	var generationConfig domain.GenerationConfig
	if snap.SystemPromptVersionID != "" && s.prompt != nil {
		resolved, err := s.prompt.ResolveConfigByVersion(ctx, snap.SystemPromptVersionID, nil, "", "")
		if err != nil {
			return nil, fmt.Errorf("render system prompt: %w", err)
		}
		systemPrompt = resolved.Rendered
		promptVersionID = resolved.VersionID
		modelProfileVersionID = resolved.ModelProfileVersionID
		generationConfig = resolved.GenerationConfig
	}
	ag, err := s.store.GetAgent(ctx, version.AgentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	for _, toolVersionID := range snap.ToolVersionIDs {
		if s.tools == nil {
			return nil, fmt.Errorf("agent has tools but tool resolver is not configured")
		}
		toolID, err := s.tools.GetToolIDByVersion(ctx, toolVersionID)
		if err != nil {
			return nil, fmt.Errorf("resolve tool version %s: %w", toolVersionID, err)
		}
		meta, err := s.tools.GetToolMeta(ctx, toolID)
		if err != nil || meta.WorkspaceID != ag.WorkspaceID {
			return nil, fmt.Errorf("tool version %s does not belong to agent workspace", toolVersionID)
		}
	}
	for _, kbID := range snap.KBIDs {
		if s.kbs == nil {
			return nil, fmt.Errorf("agent has knowledge bases but kb resolver is not configured")
		}
		ok, err := s.kbs.KBExists(ctx, ag.WorkspaceID, kbID)
		if err != nil || !ok {
			return nil, fmt.Errorf("knowledge base %s does not belong to agent workspace", kbID)
		}
	}

	// 解析订阅技能 → Runtime Spec。
	var specs []skillrunner.Spec
	if s.skills != nil {
		for _, vid := range snap.SkillVersionIDs {
			sp, err := s.skills.GetSpec(ctx, vid)
			if err != nil {
				return nil, fmt.Errorf("resolve skill version %s: %w", vid, err)
			}
			if sp.Status != "published" {
				return nil, fmt.Errorf("skill version %s is not published", vid)
			}
			if sp.WorkspaceID != ag.WorkspaceID {
				return nil, fmt.Errorf("skill version %s does not belong to agent workspace", vid)
			}
			specs = append(specs, skillrunner.Spec{
				VersionID: sp.VersionID, Name: sp.Name, Description: sp.Description, Body: sp.Body,
				AllowedTools: sp.AllowedTools, AllowedKBs: sp.AllowedKBs,
				DisableModelInvocation: sp.DisableModelInvocation,
				RequiresNetwork:        sp.RequiresNetwork,
			})
		}
	}

	return &engine.AgentSnapshot{
		ID:                    agentVersionID,
		AgentID:               version.AgentID,
		WorkspaceID:           ag.WorkspaceID,
		SystemPrompt:          systemPrompt,
		PromptVersionID:       promptVersionID,
		ModelProfileVersionID: modelProfileVersionID,
		GenerationConfig:      generationConfig,
		ToolVersionIDs:        snap.ToolVersionIDs,
		Skills:                specs,
		KBIDs:                 snap.KBIDs,
		AllowNetwork:          allowNetwork,
		MaxSteps:              snap.MaxSteps,
	}, nil
}

// GetUserPromptInputSpec 返回指定 Agent 环境当前版本绑定的 User Prompt Template。
func (s *Service) GetUserPromptInputSpec(
	ctx context.Context,
	agentID, workspaceID, agentEnv, userID string,
) (*UserPromptInputSpec, error) {
	if agentEnv == "" {
		agentEnv = prompt.EnvDev
	}
	if agentEnv != prompt.EnvDev && agentEnv != prompt.EnvStaging && agentEnv != prompt.EnvProd {
		return nil, fmt.Errorf("agent env must be dev, staging or prod")
	}
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if workspaceID != "" && ag.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("agent does not belong to current workspace")
	}
	version, err := s.store.GetAgentCurrentVersion(ctx, agentID, agentEnv)
	if err != nil {
		return nil, fmt.Errorf("get current agent version: %w", err)
	}
	var snapshot storedSnapshot
	if err := json.Unmarshal([]byte(version.SnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal agent snapshot: %w", err)
	}
	if snapshot.UserPromptID == "" {
		return &UserPromptInputSpec{Enabled: false}, nil
	}
	if s.prompt == nil {
		return nil, fmt.Errorf("prompt resolver is not configured")
	}
	metadata, err := s.prompt.GetPrompt(ctx, snapshot.UserPromptID)
	if err != nil {
		return nil, fmt.Errorf("get user prompt template: %w", err)
	}
	if metadata.WorkspaceID != ag.WorkspaceID || !isUserPromptCategory(metadata.Category) {
		return nil, fmt.Errorf("invalid user prompt template binding")
	}
	promptEnv := snapshot.PromptEnv
	if promptEnv == "" {
		promptEnv = prompt.EnvDev
	}
	promptVersionID, err := s.prompt.ResolveVersion(ctx, snapshot.UserPromptID, promptEnv, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve user prompt version: %w", err)
	}
	promptVersion, err := s.prompt.GetVersion(ctx, promptVersionID)
	if err != nil {
		return nil, fmt.Errorf("get user prompt version: %w", err)
	}
	if promptVersion.PromptID != snapshot.UserPromptID {
		return nil, fmt.Errorf("resolved user prompt version does not belong to template")
	}
	return &UserPromptInputSpec{
		Enabled: true, PromptID: metadata.ID, PromptName: metadata.Name, PromptEnv: promptEnv,
		PromptVersionID: promptVersion.ID, PromptVersion: promptVersion.Version,
		VariablesSchema: promptVersion.VariablesSchema,
	}, nil
}

// PrepareUserMessage 使用 Agent 绑定的 User Prompt Template 渲染首轮任务输入，
// 并把实际版本、变量和渲染文本写入会话运行快照。
func (s *Service) PrepareUserMessage(
	ctx context.Context,
	conversationID, agentVersionID, userID, rawMessage, requestedVersionID string,
	variables map[string]any,
) (string, string, error) {
	if variables == nil {
		return rawMessage, "", nil
	}
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return "", "", fmt.Errorf("get conversation: %w", err)
	}
	if conversation.AgentVersionID != agentVersionID {
		return "", "", fmt.Errorf("conversation agent version mismatch")
	}
	if userID != "" && conversation.UserID != userID {
		return "", "", fmt.Errorf("conversation does not belong to current user")
	}
	version, err := s.store.GetAgentVersion(ctx, agentVersionID)
	if err != nil {
		return "", "", fmt.Errorf("get agent version: %w", err)
	}
	var snapshot storedSnapshot
	if err := json.Unmarshal([]byte(version.SnapshotJSON), &snapshot); err != nil {
		return "", "", fmt.Errorf("unmarshal agent snapshot: %w", err)
	}
	if snapshot.UserPromptID == "" {
		return "", "", fmt.Errorf("agent version has no user prompt template")
	}
	if s.prompt == nil {
		return "", "", fmt.Errorf("prompt resolver is not configured")
	}

	promptVersionID := requestedVersionID
	rendered := ""
	var experimentID, experimentVariant string
	if promptVersionID != "" {
		promptVersion, err := s.prompt.GetVersion(ctx, promptVersionID)
		if err != nil {
			return "", "", fmt.Errorf("get requested user prompt version: %w", err)
		}
		if promptVersion.PromptID != snapshot.UserPromptID {
			return "", "", fmt.Errorf("requested user prompt version does not belong to agent template")
		}
		rendered, err = s.prompt.RenderByVersion(ctx, promptVersionID, variables)
		if err != nil {
			return "", "", fmt.Errorf("render user prompt template: %w", err)
		}
	} else {
		promptEnv := snapshot.PromptEnv
		if promptEnv == "" {
			promptEnv = prompt.EnvDev
		}
		resolved, err := s.prompt.ResolveConfig(ctx, snapshot.UserPromptID, promptEnv, userID, variables)
		if err != nil {
			return "", "", fmt.Errorf("resolve user prompt template: %w", err)
		}
		promptVersionID = resolved.VersionID
		rendered = resolved.Rendered
		experimentID = resolved.ExperimentID
		experimentVariant = resolved.ExperimentVariant
	}
	if extra := strings.TrimSpace(rawMessage); extra != "" {
		rendered += "\n\n# 补充说明\n\n" + extra
	}

	runtimeConfig := domain.ConversationRuntimeConfig{}
	if conversation.RuntimeConfigJSON != "" {
		if err := json.Unmarshal([]byte(conversation.RuntimeConfigJSON), &runtimeConfig); err != nil {
			return "", "", fmt.Errorf("decode conversation runtime config: %w", err)
		}
	}
	runtimeConfig.UserPromptVersionID = promptVersionID
	runtimeConfig.UserPromptVariables = variables
	runtimeConfig.RenderedUserPrompt = rendered
	runtimeConfig.UserPromptExperiment = experimentID
	runtimeConfig.UserPromptVariant = experimentVariant
	runtimeJSON, err := json.Marshal(runtimeConfig)
	if err != nil {
		return "", "", err
	}
	if err := s.store.UpdateConversationRuntimeConfig(ctx, conversationID, string(runtimeJSON)); err != nil {
		return "", "", fmt.Errorf("update conversation runtime config: %w", err)
	}
	return rendered, promptVersionID, nil
}

// CreateConversation 新建会话并 pin 当前 env 指向的 AgentVersion（实现 engine.Platform）。
func (s *Service) CreateConversation(ctx context.Context, agentID, userID string) (*domain.Conversation, error) {
	return s.CreateConversationInEnv(ctx, agentID, "dev", userID)
}

// CreateConversationInEnv 新建会话并 pin 指定环境当前 AgentVersion。
func (s *Service) CreateConversationInEnv(ctx context.Context, agentID, env, userID string) (*domain.Conversation, error) {
	if env == "" {
		env = "dev"
	}
	if env != "dev" && env != "staging" && env != "prod" {
		return nil, fmt.Errorf("agent env must be dev, staging or prod")
	}
	ver, err := s.store.GetAgentCurrentVersion(ctx, agentID, env)
	if err != nil {
		return nil, fmt.Errorf("get current agent version: %w", err)
	}
	return s.newConversation(ctx, ver, env, userID)
}

// CreateConversationWithVersion 新建会话并 pin 到【调用方指定的 AgentVersion】(实现 engine.Platform)。
//
// 与 CreateConversation(取当前版本)相对:Team 跑成员时用它,把会话钉死到团队快照里
// pin 的成员 Agent 固定版本，运行中不跟随成员最新版变化（不可变快照贯彻到编排）。
func (s *Service) CreateConversationWithVersion(ctx context.Context, agentVersionID, userID string) (*domain.Conversation, error) {
	ver, err := s.store.GetAgentVersion(ctx, agentVersionID)
	if err != nil {
		return nil, fmt.Errorf("get agent version: %w", err)
	}
	return s.newConversation(ctx, ver, "dev", userID)
}

// newConversation 用一个已解析的 AgentVersion 建会话(两个入口共用)。
func (s *Service) newConversation(ctx context.Context, ver *domain.AgentVersion, env, userID string) (*domain.Conversation, error) {
	var snap storedSnapshot
	if err := json.Unmarshal([]byte(ver.SnapshotJSON), &snap); err != nil {
		return nil, fmt.Errorf("unmarshal agent snapshot: %w", err)
	}
	if env == "" {
		env = "dev"
	}
	runtimeConfig := domain.ConversationRuntimeConfig{Environment: env}
	if s.prompt != nil {
		var resolved *prompt.ResolvedConfig
		var err error
		switch {
		case snap.SystemPromptID != "":
			env := snap.PromptEnv
			if env == "" {
				env = prompt.EnvDev
			}
			resolved, err = s.prompt.ResolveConfig(ctx, snap.SystemPromptID, env, userID, nil)
		case snap.SystemPromptVersionID != "":
			resolved, err = s.prompt.ResolveConfigByVersion(ctx, snap.SystemPromptVersionID, nil, "", "")
		}
		if err != nil {
			return nil, fmt.Errorf("resolve prompt config: %w", err)
		}
		if resolved != nil {
			runtimeConfig = domain.ConversationRuntimeConfig{
				Environment:  env,
				SystemPrompt: resolved.Rendered, PromptVersionID: resolved.VersionID,
				ModelProfileVersionID: resolved.ModelProfileVersionID,
				GenerationConfig:      resolved.GenerationConfig, ExperimentID: resolved.ExperimentID,
				ExperimentVariant: resolved.ExperimentVariant,
			}
		}
	}
	runtimeJSON, err := json.Marshal(runtimeConfig)
	if err != nil {
		return nil, err
	}
	ag, err := s.store.GetAgent(ctx, ver.AgentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	conversation := &domain.Conversation{
		ID:                util.GenerateID(),
		AgentID:           ver.AgentID,
		AgentVersionID:    ver.ID, // pin：老对话不切版本（讲义 §14.6）
		WorkspaceID:       ag.WorkspaceID,
		UserID:            userID,
		Status:            "active",
		Classification:    "internal",
		RuntimeConfigJSON: string(runtimeJSON),
		StartedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if err := s.store.CreateConversation(ctx, conversation); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return conversation, nil
}

// GetAgentCurrentVersionID 返回 agent 在指定 env 的当前版本 ID
// （供 Team 在建团队时 pin 成员 agent 版本,满足 team.AgentResolver）。
func (s *Service) GetAgentCurrentVersionID(ctx context.Context, agentID, env string) (string, error) {
	ver, err := s.store.GetAgentCurrentVersion(ctx, agentID, env)
	if err != nil {
		return "", fmt.Errorf("get current agent version: %w", err)
	}
	return ver.ID, nil
}

// ValidateAgentVersion 校验 Agent、不可变版本与 Workspace 的归属关系。
// Team 等跨资源装配入口必须在写入快照前调用，避免把其他 Workspace 的 Agent
// 或版本引用带入当前 Workspace。
func (s *Service) ValidateAgentVersion(ctx context.Context, agentID, versionID, workspaceID string) error {
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if workspaceID == "" || ag.WorkspaceID != workspaceID {
		return fmt.Errorf("agent does not belong to current workspace")
	}
	if versionID == "" {
		return nil
	}
	version, err := s.store.GetAgentVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get agent version: %w", err)
	}
	if version.AgentID != agentID {
		return fmt.Errorf("agent version does not belong to agent")
	}
	return nil
}

// ListAgents 列出工作空间下的 Agents。
func (s *Service) ListAgents(ctx context.Context, workspaceID string) ([]*domain.Agent, error) {
	return s.store.ListAgents(ctx, workspaceID)
}

// GetAgent 返回单个 Agent。
func (s *Service) GetAgent(ctx context.Context, agentID string) (*domain.Agent, error) {
	return s.store.GetAgent(ctx, agentID)
}

// GetAgentForWorkspace 返回当前 Workspace 内的 Agent，资源 ID 不作为授权凭据。
func (s *Service) GetAgentForWorkspace(ctx context.Context, agentID, workspaceID string) (*domain.Agent, error) {
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if workspaceID == "" || ag.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("agent not found")
	}
	return ag, nil
}

// LoadConversation 取会话（实现 engine.Platform）。
func (s *Service) LoadConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	return s.store.GetConversation(ctx, conversationID)
}

// RecordConversationTraceID 保存会话最近一次运行的 Trace ID，供历史会话恢复 Langfuse 链接。
// 会话每一轮都有独立 Trace；详情页展示最近一轮。
func (s *Service) RecordConversationTraceID(ctx context.Context, conversationID, traceID string) error {
	if strings.TrimSpace(traceID) == "" {
		return nil
	}
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("get conversation: %w", err)
	}
	runtimeConfig := domain.ConversationRuntimeConfig{}
	if conversation.RuntimeConfigJSON != "" {
		if err := json.Unmarshal([]byte(conversation.RuntimeConfigJSON), &runtimeConfig); err != nil {
			return fmt.Errorf("decode conversation runtime config: %w", err)
		}
	}
	runtimeConfig.LatestTraceID = traceID
	runtimeJSON, err := json.Marshal(runtimeConfig)
	if err != nil {
		return fmt.Errorf("encode conversation runtime config: %w", err)
	}
	if err := s.store.UpdateConversationRuntimeConfig(ctx, conversationID, string(runtimeJSON)); err != nil {
		return fmt.Errorf("update conversation runtime config: %w", err)
	}
	return nil
}

// ListConversations 返回当前用户在工作空间内的会话历史。
func (s *Service) ListConversations(
	ctx context.Context, workspaceID, userID, agentID string, limit, offset int32,
) ([]*domain.Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListConversations(ctx, workspaceID, userID, agentID, limit, offset)
}

// LoadConversationForUser 在控制面读取历史时校验工作空间与用户归属。
func (s *Service) LoadConversationForUser(
	ctx context.Context, conversationID, workspaceID, userID string,
) (*domain.Conversation, error) {
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conversation.WorkspaceID != workspaceID || conversation.UserID != userID {
		return nil, fmt.Errorf("conversation not found")
	}
	return conversation, nil
}

// LoadConversationMessages 加载会话消息（实现 engine.Platform）。
func (s *Service) LoadConversationMessages(ctx context.Context, conversationID string) ([]*domain.Message, error) {
	return s.store.GetConversationMessages(ctx, conversationID)
}

// AppendMessage 追加消息（实现 engine.Platform）。
func (s *Service) AppendMessage(ctx context.Context, conversationID, role, content string) error {
	message := &domain.Message{
		ID:             util.GenerateID(),
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		CreatedAt:      time.Now(),
	}
	return s.store.CreateMessage(ctx, message)
}

func (s *Service) ClaimConversationTurn(ctx context.Context, conversationID string, resume bool) (string, error) {
	store, ok := s.store.(conversationTurnStore)
	if !ok {
		return "", fmt.Errorf("conversation turn coordination is unavailable")
	}
	return store.ClaimConversationTurn(ctx, conversationID, resume)
}

func (s *Service) RenewConversationTurn(ctx context.Context, conversationID, token string) error {
	store, ok := s.store.(conversationTurnStore)
	if !ok {
		return fmt.Errorf("conversation turn coordination is unavailable")
	}
	return store.RenewConversationTurn(ctx, conversationID, token)
}

func (s *Service) CommitConversationTurn(
	ctx context.Context, conversationID, token string, messages []*domain.Message, nextStatus string,
) error {
	store, ok := s.store.(conversationTurnStore)
	if !ok {
		return fmt.Errorf("conversation turn coordination is unavailable")
	}
	return store.CommitConversationTurn(ctx, conversationID, token, messages, nextStatus)
}

func (s *Service) ReleaseConversationTurn(ctx context.Context, conversationID, token, nextStatus string) error {
	store, ok := s.store.(conversationTurnStore)
	if !ok {
		return fmt.Errorf("conversation turn coordination is unavailable")
	}
	return store.ReleaseConversationTurn(ctx, conversationID, token, nextStatus)
}
