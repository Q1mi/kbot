// Package engine 定义 Agent 数据面的稳定运行边界。
package engine

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

// Generator 隔离模型框架与 Agent 循环。
// 后续可以接入真实 Eino ChatModel，也可以在测试中注入脚本模型。
type Generator interface {
	Generate(
		ctx context.Context,
		messages []*schema.Message,
		tools []*schema.ToolInfo,
	) (*schema.Message, error)
}

// Platform 是 Runtime 读取控制面数据所需的最小接口。
// Runtime 只按 Conversation 中固定的版本读取快照。
type Platform interface {
	LoadConversation(ctx context.Context, conversationID string) (*domain.Conversation, error)
	GetAgentSnapshotByVersion(ctx context.Context, agentVersionID string) (*AgentSnapshot, error)
}

// AgentSnapshot 是 Runtime 已解析好的固定配置。
// 后续课程会逐步加入 Model、Prompt、Tool、Skill 和知识库版本。
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

// Engine 只依赖稳定接口，不直接依赖控制面的具体存储实现。
type Engine struct {
	platform  Platform
	gen       Generator
	tools     ToolRuntime
	prompts   PromptRenderer
	profiles  ModelProfileResolver
	skills    SkillResolver
	approvals ApprovalCreator
	guard     RuntimeGuard
	audit     AuditSink
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

type RoutedGenerator interface {
	GenerateWithProfile(ctx context.Context, profile modelconfig.ProfileVersion, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error)
}

type SkillResolver interface {
	Resolve(ctx context.Context, workspaceID, versionID string) (skill.Version, error)
}

type ConversationMessageStore interface {
	ListMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error)
	AppendMessage(ctx context.Context, workspaceID, conversationID, role, content string) error
}

type ApprovalCreator interface {
	Create(ctx context.Context, request approval.Request) (*approval.Request, error)
}

type RuntimeGuard interface {
	Evaluate(ctx context.Context, workspaceID, hook, text string) (guard.Decision, error)
}

type AuditSink interface {
	Append(ctx context.Context, event audit.Event) (audit.Event, error)
}

// New 创建 Agent Runtime。
func New(platform Platform, gen Generator) *Engine {
	return &Engine{platform: platform, gen: gen}
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

func (e *Engine) WithApprovals(approvals ApprovalCreator) *Engine {
	e.approvals = approvals
	return e
}

func (e *Engine) WithGuard(runtimeGuard RuntimeGuard) *Engine {
	e.guard = runtimeGuard
	return e
}

func (e *Engine) WithAudit(sink AuditSink) *Engine {
	e.audit = sink
	return e
}

// ResolveSnapshot 按会话固定的 AgentVersion 解析运行快照。
// 该方法会在后续课程中进入完整 ChatStream 调用链。
func (e *Engine) ResolveSnapshot(
	ctx context.Context,
	conversationID string,
) (*AgentSnapshot, error) {
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
		return nil, fmt.Errorf(
			"snapshot version mismatch: want %s, got %s",
			conversation.AgentVersionID,
			snapshot.ID,
		)
	}
	if conversation.WorkspaceID != "" && snapshot.WorkspaceID != "" && conversation.WorkspaceID != snapshot.WorkspaceID {
		return nil, fmt.Errorf("conversation and agent snapshot belong to different workspaces")
	}
	return snapshot, nil
}
