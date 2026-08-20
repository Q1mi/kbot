package engine

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

type sequenceChatModel struct {
	calls         int
	sawToolResult bool
	alwaysTool    bool
}

func (m *sequenceChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	for _, message := range messages {
		if message.Role == schema.Tool {
			m.sawToolResult = true
		}
	}
	if m.calls == 1 || m.alwaysTool {
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-1", Type: "function",
			Function: schema.FunctionCall{Name: "refund", Arguments: `{"order_id":"ORD-1"}`},
		}}), nil
	}
	return schema.AssistantMessage("退款已提交", nil), nil
}

func (m *sequenceChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type fakeToolExecutor struct{ call tooling.Call }

func (f *fakeToolExecutor) Execute(_ context.Context, call tooling.Call) (tooling.Result, error) {
	f.call = call
	return tooling.Result{StatusCode: 200, Body: []byte(`{"status":"submitted"}`)}, nil
}

func refundBinding() ToolBinding {
	return ToolBinding{Name: "refund", VersionID: "refund-v1", Info: &schema.ToolInfo{
		Name: "refund", Desc: "submit refund",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"order_id": {Type: schema.String, Required: true},
		}),
	}}
}

func TestADKRunnerFeedsToolResultBackToModel(t *testing.T) {
	chatModel := &sequenceChatModel{}
	executor := &fakeToolExecutor{}
	runner := NewADKRunner(chatModel, executor, "ws-1")
	var events []string
	answer, err := runner.Run(context.Background(), []*schema.Message{schema.UserMessage("退款")}, []ToolBinding{refundBinding()}, 4, func(event Event) error {
		events = append(events, event.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answer.Content != "退款已提交" || !chatModel.sawToolResult {
		t.Fatalf("answer=%q sawTool=%v", answer.Content, chatModel.sawToolResult)
	}
	if executor.call.ToolVersionID != "refund-v1" || executor.call.IdempotencyKey != "react:call-1" {
		t.Fatalf("call = %+v", executor.call)
	}
	if len(events) != 2 || events[0] != "tool_started" || events[1] != "tool_finished" {
		t.Fatalf("events = %v", events)
	}
}

func TestADKRunnerStopsAtMaxSteps(t *testing.T) {
	chatModel := &sequenceChatModel{alwaysTool: true}
	_, err := NewADKRunner(chatModel, &fakeToolExecutor{}, "ws").Run(
		context.Background(), nil, []ToolBinding{refundBinding()}, 2, nil,
	)
	if err == nil {
		t.Fatal("expected max-step error")
	}
}
