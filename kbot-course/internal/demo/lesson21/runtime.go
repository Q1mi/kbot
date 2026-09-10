// Package lesson21 创建使用正式 Engine、Tool Registry 和 Executor 的独立演示环境。
package lesson21

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/demo/logistics"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/audit"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

const (
	WorkspaceID    = "lesson21-workspace"
	AgentVersionID = "lesson21-agent-v1"
	ToolVersionID  = "logistics-a2a-tool-v1"
	ToolName       = "query_logistics"
)

type Runtime struct {
	Agents  *agent.Service
	Engine  *engine.Engine
	AgentID string
}

func NewRuntime(ctx context.Context, cardURL string, chatModel model.BaseChatModel) (*Runtime, error) {
	if chatModel == nil {
		chatModel = DemoModel{}
	}
	agents := agent.NewService()
	item, err := agents.CreateAgent(ctx, WorkspaceID, "第 21 课物流助手", "")
	if err != nil {
		return nil, err
	}
	registry := platformtool.NewRegistry()
	err = registry.Register(ctx, platformtool.Version{
		ID: ToolVersionID, ToolID: "logistics-a2a", WorkspaceID: WorkspaceID, Name: ToolName,
		SourceType: "a2a", Description: "查询演示订单 CB-1001、CB-1002、CB-1003 的物流信息，远端版本固定为 logistics-v1。",
		Endpoint: cardURL, Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string","minLength":1},"agent_version_id":{"type":"string","enum":["logistics-v1"]}},"required":["message","agent_version_id"],"additionalProperties":false}`),
	})
	if err != nil {
		return nil, err
	}
	err = agents.Publish(ctx, domain.AgentVersion{ID: AgentVersionID, AgentID: item.ID, WorkspaceID: WorkspaceID}, engine.AgentSnapshot{
		ID: AgentVersionID, AgentID: item.ID, WorkspaceID: WorkspaceID, MaxSteps: 6,
		SystemPrompt:   "你是 kbot 第 21 课的中文助手。遇到物流问题调用 query_logistics，agent_version_id 必须使用 logistics-v1。返回时说明数据为演示数据，不编造物流状态。其他问题正常回答。",
		ToolVersionIDs: []string{ToolVersionID},
	})
	if err != nil {
		return nil, err
	}
	if err = agents.Promote(ctx, WorkspaceID, item.ID, "dev", AgentVersionID); err != nil {
		return nil, err
	}
	// 演示仅允许访问本机 Mock。扩展域名时需同时评审工具版本及网络访问策略。
	executor := tooling.NewExecutor(registry, nil, "127.0.0.1", "localhost")
	runtime := engine.New(agents, chatModel).WithTools(executor).
		WithGuard(guard.NewService(guard.NewPipeline(guard.MaxLengthRule{MaxRunes: 8000}, guard.InjectionRule{}))).
		WithAudit(audit.NewLedger())
	return &Runtime{Agents: agents, Engine: runtime, AgentID: item.ID}, nil
}

// DemoModel 用确定性 tool_call 演示真实工具执行链，运行它无需大模型 API Key。
// 真实模型通过 NewRuntime 的 chatModel 参数替换。
type DemoModel struct{}

func (DemoModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages are required")
	}
	last := messages[len(messages)-1]
	if last.Role == schema.Tool {
		var result tooling.A2AResult
		if err := json.Unmarshal([]byte(last.Content), &result); err != nil {
			return nil, fmt.Errorf("decode logistics tool result: %w", err)
		}
		if result.Content == "" {
			return nil, fmt.Errorf("logistics tool returned no text")
		}
		return schema.AssistantMessage(result.Content, nil), nil
	}
	arguments, _ := json.Marshal(map[string]string{"message": last.Content, "agent_version_id": logistics.Version})
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: "logistics-" + rand.Text(), Type: "function", Function: schema.FunctionCall{Name: ToolName, Arguments: string(arguments)},
	}}), nil
}

func (m DemoModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}
