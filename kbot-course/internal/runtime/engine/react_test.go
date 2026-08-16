package engine

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/cloudwego/eino/schema"
)

type sequenceGenerator struct {
	calls         int
	sawToolResult bool
	systemPrompt  string
	toolCount     int
}

func (g *sequenceGenerator) Generate(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	g.calls++
	g.toolCount = len(tools)
	if len(messages) > 0 {
		g.systemPrompt = messages[0].Content
	}
	for _, message := range messages {
		if message.Role == schema.Tool {
			g.sawToolResult = true
		}
	}
	if g.calls == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "refund", Arguments: `{"order_id":"ORD-1"}`}}}), nil
	}
	return schema.AssistantMessage("退款已提交", nil), nil
}

type fakeToolExecutor struct{ call tooling.Call }

func (f *fakeToolExecutor) Execute(_ context.Context, call tooling.Call) (tooling.Result, error) {
	f.call = call
	return tooling.Result{StatusCode: 200, Body: []byte(`{"status":"submitted"}`)}, nil
}

func TestReActRunnerFeedsToolResultBackToModel(t *testing.T) {
	gen := &sequenceGenerator{}
	executor := &fakeToolExecutor{}
	runner := NewReActRunner(gen, executor, "ws-1")
	var events []string
	answer, err := runner.Run(context.Background(), []*schema.Message{schema.UserMessage("退款")}, []ToolBinding{{Name: "refund", VersionID: "refund-v1", Info: &schema.ToolInfo{}}}, 4, func(event Event) error {
		events = append(events, event.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answer.Content != "退款已提交" || !gen.sawToolResult {
		t.Fatalf("answer=%q sawTool=%v", answer.Content, gen.sawToolResult)
	}
	if executor.call.ToolVersionID != "refund-v1" || executor.call.IdempotencyKey != "react:call-1" {
		t.Fatalf("call = %+v", executor.call)
	}
	if len(events) != 2 || events[0] != "tool_started" || events[1] != "tool_finished" {
		t.Fatalf("events = %v", events)
	}
}

func TestReActRunnerStopsAtMaxSteps(t *testing.T) {
	gen := GeneratorFunc(func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "again", Function: schema.FunctionCall{Name: "loop", Arguments: `{}`}}}), nil
	})
	_, err := NewReActRunner(gen, &fakeToolExecutor{}, "ws").Run(context.Background(), nil, []ToolBinding{{Name: "loop", VersionID: "v1", Info: &schema.ToolInfo{}}}, 2, nil)
	if err == nil {
		t.Fatal("expected max-step error")
	}
}

type GeneratorFunc func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error)

func (f GeneratorFunc) Generate(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	return f(ctx, messages, tools)
}

func TestSkillKnowledgeBaseAllowlistIsEnforcedBeforeExecution(t *testing.T) {
	binding := ToolBinding{Name: "search_knowledge_base", KBScoped: true, RestrictKBs: true, AllowedKBs: []string{"kb-allowed"}}
	if err := validateBindingCall(binding, []byte(`{"kb_id":"kb-allowed"}`)); err != nil {
		t.Fatal(err)
	}
	if err := validateBindingCall(binding, []byte(`{"kb_id":"kb-other"}`)); err == nil {
		t.Fatal("expected cross-KB call to fail")
	}
}
