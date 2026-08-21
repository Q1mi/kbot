package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/runtime/guard"
	runtimeteam "github.com/Q1mi/kbot/internal/runtime/team"
)

type teamScriptedModel struct {
	mu       sync.Mutex
	replies  []*schema.Message
	index    int
	received [][]*schema.Message
}

func (m *teamScriptedModel) Generate(
	_ context.Context, messages []*schema.Message, _ ...model.Option,
) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, append([]*schema.Message(nil), messages...))
	result := m.replies[m.index]
	if m.index < len(m.replies)-1 {
		m.index++
	}
	return result, nil
}

func TestRunSupervisorTeamGuardsInputAndOutput(t *testing.T) {
	platform := &fakePlatform{snapshots: map[string]*AgentSnapshot{
		"supervisor-v1": {
			ID: "supervisor-v1", AgentID: "supervisor", WorkspaceID: "w1",
			SystemPrompt: "你是客服主管", MaxSteps: 2,
		},
	}}
	chatModel := &teamScriptedModel{replies: []*schema.Message{
		schema.AssistantMessage("请联系 13800138000", nil),
	}}
	runtimeGuard := guard.NewService(guard.NewPipeline(guard.PIIRule{}))
	runtime := New(platform, chatModel).WithGuard(runtimeGuard)
	worker := runtimeteam.Member{AgentID: "worker", AgentVersionID: "worker-v1", Role: "worker"}

	answer, steps, err := runtime.RunSupervisorTeam(
		context.Background(),
		runtimeteam.Member{AgentID: "supervisor", AgentVersionID: "supervisor-v1", Role: "supervisor"},
		[]runtimeteam.Member{worker}, "联系 student@example.com",
		func(context.Context, runtimeteam.Member, string) (string, error) {
			return "unused", nil
		},
	)
	if err != nil {
		t.Fatalf("run supervisor team: %v", err)
	}
	if answer != "请联系 [PHONE]" {
		t.Fatalf("answer = %q", answer)
	}
	if len(steps) != 1 || steps[0].Input != "联系 [EMAIL]" || steps[0].Output != answer {
		t.Fatalf("unexpected steps: %+v", steps)
	}
	foundSanitizedInput := false
	for _, call := range chatModel.received {
		for _, message := range call {
			if message.Role == schema.User && message.Content == "联系 [EMAIL]" {
				foundSanitizedInput = true
			}
		}
	}
	if !foundSanitizedInput {
		t.Fatalf("supervisor model did not receive sanitized input: %+v", chatModel.received)
	}
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

func TestRunSupervisorTeamPropagatesMemberApprovalPause(t *testing.T) {
	platform := &fakePlatform{snapshots: map[string]*AgentSnapshot{
		"supervisor-v1": {
			ID: "supervisor-v1", AgentID: "supervisor", WorkspaceID: "w1",
			SystemPrompt: "你是客服主管", MaxSteps: 2,
		},
	}}
	chatModel := &teamScriptedModel{replies: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "delegate-1", Function: schema.FunctionCall{Name: "billing", Arguments: `{"request":"执行退款"}`},
		}}),
	}}
	runtime := New(platform, chatModel)
	expected := &AwaitingApprovalError{
		ApprovalID: "approval-1", ConversationID: "conversation-1", ToolName: "refund",
	}
	_, _, err := runtime.RunSupervisorTeam(
		context.Background(),
		runtimeteam.Member{AgentID: "supervisor", AgentVersionID: "supervisor-v1", Role: "supervisor"},
		[]runtimeteam.Member{{AgentID: "billing-agent", AgentVersionID: "billing-v1", Role: "billing"}},
		"处理退款",
		func(context.Context, runtimeteam.Member, string) (string, error) {
			return "", expected
		},
	)
	var approvalErr *AwaitingApprovalError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected AwaitingApprovalError, got %T: %v", err, err)
	}
	if approvalErr != expected {
		t.Fatalf("approval error identity was not preserved: got=%p want=%p", approvalErr, expected)
	}
}
