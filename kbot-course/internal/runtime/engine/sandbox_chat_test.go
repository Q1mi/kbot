package engine

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

type codeToolGenerator struct {
	calls         int
	sawToolResult bool
}

func (g *codeToolGenerator) Generate(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
	g.calls++
	for _, message := range messages {
		if message.Role == schema.Tool && message.Content == "42\n" {
			g.sawToolResult = true
		}
	}
	if g.calls == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID: "code-call-1", Type: "function",
			Function: schema.FunctionCall{Name: "code_run_python", Arguments: `{"code":"print(6 * 7)"}`},
		}}), nil
	}
	return schema.AssistantMessage("计算结果是 42", nil), nil
}

type classroomSandbox struct {
	language string
	code     string
}

func (s *classroomSandbox) Run(_ context.Context, language, code string) (string, error) {
	s.language, s.code = language, code
	return "42\n", nil
}

func TestChatStreamRunsPinnedCodeToolThroughSandbox(t *testing.T) {
	registry := platformtool.NewRegistry()
	if err := registry.Register(t.Context(), platformtool.Version{
		ID: "python-v1", WorkspaceID: "ws-1", Name: "code_run_python",
		SourceType: "code_execution", Endpoint: "python", Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`),
	}); err != nil {
		t.Fatal(err)
	}
	controlPlane := &fakePlatform{
		conversation: &domain.Conversation{ID: "c1", WorkspaceID: "ws-1", AgentVersionID: "v1"},
		snapshots: map[string]*AgentSnapshot{"v1": {
			ID: "v1", WorkspaceID: "ws-1", SystemPrompt: "Use Python when calculation is required.",
			MaxSteps: 4, ToolVersionIDs: []string{"python-v1"},
		}},
	}
	generator := &codeToolGenerator{}
	runner := &classroomSandbox{}
	runtime := New(controlPlane, generator).WithTools(tooling.NewExecutor(registry, nil).WithSandbox(runner))

	var answer string
	if err := runtime.ChatStream(t.Context(), ChatRequest{
		ConversationID: "c1", WorkspaceID: "ws-1", Message: "计算 6 * 7",
	}, func(event Event) error {
		if event.Type == "answer_done" {
			answer = event.Text
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runner.language != "python" || runner.code != "print(6 * 7)" {
		t.Fatalf("sandbox call = %q / %q", runner.language, runner.code)
	}
	if !generator.sawToolResult || answer != "计算结果是 42" {
		t.Fatalf("saw tool result = %t, answer = %q", generator.sawToolResult, answer)
	}
}
