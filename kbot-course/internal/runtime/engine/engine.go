// Package engine 定义 Agent 数据面的稳定运行边界。
package engine

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
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
	ID           string
	AgentID      string
	WorkspaceID  string
	SystemPrompt string
	MaxSteps     int
}

// Engine 只依赖稳定接口，不直接依赖控制面的具体存储实现。
type Engine struct {
	platform Platform
	gen      Generator
}

// New 创建 Agent Runtime。
func New(platform Platform, gen Generator) *Engine {
	return &Engine{platform: platform, gen: gen}
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
	return snapshot, nil
}
