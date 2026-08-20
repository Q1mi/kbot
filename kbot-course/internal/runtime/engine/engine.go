// Package engine 定义 Agent 数据面的稳定运行边界。
package engine

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

// Platform 是 Runtime 读取控制面数据所需的最小接口。
type Platform interface {
	LoadConversation(ctx context.Context, conversationID string) (*domain.Conversation, error)
	GetAgentSnapshotByVersion(ctx context.Context, agentVersionID string) (*AgentSnapshot, error)
}

type AgentSnapshot struct {
	ID                    string
	AgentID               string
	WorkspaceID           string
	SystemPrompt          string
	MaxSteps              int
	PromptVersionID       string
	ModelProfileVersionID string
	ToolVersionIDs        []string
	SkillVersionIDs       []string
	KnowledgeVersionIDs   []string
}

type executionPlanner interface {
	PrepareExecution(context.Context, modelconfig.ProfileVersion) (*llm.ExecutionPlan, error)
}

type Engine struct {
	platform  Platform
	model     model.BaseChatModel
	planner   executionPlanner
	tools     ToolRuntime
	prompts   PromptRenderer
	profiles  ModelProfileResolver
	skills    SkillResolver
	approvals ApprovalGate
}

type ToolRuntime interface {
	Bind(ctx context.Context, workspaceID string, versionIDs []string) ([]tooling.Binding, error)
	Execute(ctx context.Context, call tooling.Call) (tooling.Result, error)
}

type PromptRenderer interface {
	Render(ctx context.Context, workspaceID, versionID string, variables map[string]string) (string, error)
}

type ModelProfileResolver interface {
	Resolve(ctx context.Context, workspaceID, versionID string) (modelconfig.ProfileVersion, error)
}

type SkillResolver interface {
	Resolve(ctx context.Context, workspaceID, versionID string) (skill.Version, error)
}

type ConversationMessageStore interface {
	ListMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error)
	AppendMessage(ctx context.Context, workspaceID, conversationID, role, content string) error
}

func New(platform Platform, chatModel model.BaseChatModel) *Engine {
	engine := &Engine{platform: platform, model: chatModel}
	engine.planner, _ = chatModel.(executionPlanner)
	return engine
}

func (e *Engine) WithTools(tools ToolRuntime) *Engine {
	e.tools = tools
	return e
}

func (e *Engine) WithRuntimeConfig(prompts PromptRenderer, profiles ModelProfileResolver) *Engine {
	e.prompts, e.profiles = prompts, profiles
	return e
}

func (e *Engine) WithSkills(skills SkillResolver) *Engine {
	e.skills = skills
	return e
}

func (e *Engine) WithApprovals(approvals ApprovalGate) *Engine {
	e.approvals = approvals
	return e
}

func (e *Engine) ResolveSnapshot(ctx context.Context, conversationID string) (*AgentSnapshot, error) {
	if e.platform == nil {
		return nil, fmt.Errorf("platform is required")
	}
	conversation, err := e.platform.LoadConversation(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
	}
	if conversation.AgentVersionID == "" {
		return nil, fmt.Errorf("conversation has no pinned agent version")
	}
	snapshot, err := e.platform.GetAgentSnapshotByVersion(ctx, conversation.AgentVersionID)
	if err != nil {
		return nil, fmt.Errorf("get agent snapshot: %w", err)
	}
	if snapshot.ID != conversation.AgentVersionID {
		return nil, fmt.Errorf("snapshot version mismatch: want %s, got %s", conversation.AgentVersionID, snapshot.ID)
	}
	if conversation.WorkspaceID != "" && snapshot.WorkspaceID != "" && conversation.WorkspaceID != snapshot.WorkspaceID {
		return nil, fmt.Errorf("conversation and agent snapshot belong to different workspaces")
	}
	return snapshot, nil
}
