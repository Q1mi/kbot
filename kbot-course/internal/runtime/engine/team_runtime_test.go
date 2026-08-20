package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	runtimeteam "github.com/Q1mi/kbot/internal/runtime/team"
)

type teamScriptedModel struct {
	mu      sync.Mutex
	replies []*schema.Message
	index   int
}

func (m *teamScriptedModel) Generate(
	_ context.Context, _ []*schema.Message, _ ...model.Option,
) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := m.replies[m.index]
	if m.index < len(m.replies)-1 {
		m.index++
	}
	return result, nil
}

func (m *teamScriptedModel) Stream(
	ctx context.Context, messages []*schema.Message, options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func TestRunSupervisorTeamUsesEinoAgentTool(t *testing.T) {
	platform := &fakePlatform{snapshots: map[string]*AgentSnapshot{
		"supervisor-v1": {
			ID: "supervisor-v1", AgentID: "supervisor", WorkspaceID: "w1",
			SystemPrompt: "你是客服主管", MaxSteps: 4,
		},
	}}
	chatModel := &teamScriptedModel{replies: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "delegate-1", Function: schema.FunctionCall{Name: "billing", Arguments: `{"request":"核对退款状态"}`},
		}}),
		schema.AssistantMessage("已核对，款项可以原路退回。", nil),
	}}
	runtime := New(platform, chatModel)
	worker := runtimeteam.Member{AgentID: "billing-agent", AgentVersionID: "billing-v1", Role: "billing"}
	answer, steps, err := runtime.RunSupervisorTeam(
		context.Background(),
		runtimeteam.Member{AgentID: "supervisor", AgentVersionID: "supervisor-v1", Role: "supervisor"},
		[]runtimeteam.Member{worker}, "帮我确认退款",
		func(_ context.Context, member runtimeteam.Member, input string) (string, error) {
			if member.AgentVersionID != "billing-v1" || input != "核对退款状态" {
				t.Fatalf("member=%+v input=%q", member, input)
			}
			return "退款状态正常，可原路退回。", nil
		},
	)
	if err != nil {
		t.Fatalf("run supervisor team: %v", err)
	}
	if answer != "已核对，款项可以原路退回。" {
		t.Fatalf("answer = %q", answer)
	}
	if len(steps) != 2 || steps[0].Role != "billing" || steps[1].Role != "supervisor" {
		t.Fatalf("unexpected steps: %+v", steps)
	}
}
