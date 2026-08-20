package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type replyChatModel struct{ answer string }

func (g replyChatModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	answer := g.answer
	if answer == "" {
		answer = "hello"
	}
	return schema.AssistantMessage(answer, nil), nil
}

func (g replyChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := g.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	runes := []rune(response.Content)
	middle := len(runes) / 2
	if middle == 0 {
		middle = len(runes)
	}
	return schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage(string(runes[:middle]), nil),
		schema.AssistantMessage(string(runes[middle:]), nil),
	}), nil
}

func TestChatStreamRunsPinnedRESTToolEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "react:call-1" {
			t.Errorf("idempotency key = %q", r.Header.Get("Idempotency-Key"))
		}
		_, _ = w.Write([]byte(`{"status":"submitted"}`))
	}))
	defer server.Close()
	registry := platformtool.NewRegistry()
	if err := registry.Register(t.Context(), platformtool.Version{
		ID: "refund-v1", WorkspaceID: "ws-1", Name: "refund", Description: "submit refund",
		Endpoint: server.URL, Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`),
	}); err != nil {
		t.Fatal(err)
	}
	controlPlane := &fakePlatform{
		conversation: &domain.Conversation{ID: "c1", WorkspaceID: "ws-1", AgentVersionID: "v1"},
		snapshots: map[string]*AgentSnapshot{"v1": {
			ID: "v1", WorkspaceID: "ws-1", SystemPrompt: "help", MaxSteps: 4,
			ToolVersionIDs: []string{"refund-v1"},
		}},
	}
	chatModel := &sequenceChatModel{}
	runtime := New(controlPlane, chatModel).WithTools(tooling.NewExecutor(registry, server.Client(), "127.0.0.1"))
	var events []string
	err := runtime.ChatStream(t.Context(), ChatRequest{ConversationID: "c1", WorkspaceID: "ws-1", Message: "退款"}, func(event Event) error {
		events = append(events, event.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	if !chatModel.sawToolResult {
		t.Fatal("model did not receive the REST tool result")
	}
	if got := strings.Join(events, ","); !strings.Contains(got, "tool_started,tool_finished") || !strings.Contains(got, "answer_done") {
		t.Fatalf("events = %v", events)
	}
}

func TestChatStreamEmitsIncrementalAnswerDeltas(t *testing.T) {
	controlPlane := &fakePlatform{
		conversation: &domain.Conversation{ID: "c1", AgentVersionID: "v1"},
		snapshots:    map[string]*AgentSnapshot{"v1": {ID: "v1", SystemPrompt: "help"}},
	}
	runtime := New(controlPlane, replyChatModel{answer: "这是一个足够长的流式回答"})
	var deltas []string
	if err := runtime.ChatStream(context.Background(), ChatRequest{ConversationID: "c1", Message: "hi"}, func(event Event) error {
		if event.Type == "answer_delta" {
			deltas = append(deltas, event.Text)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(deltas) < 2 {
		t.Fatalf("answer deltas = %q, want multiple incremental chunks", deltas)
	}
	if got := strings.Join(deltas, ""); got != "这是一个足够长的流式回答" {
		t.Fatalf("joined deltas = %q", got)
	}
}

func TestChatStreamEventOrder(t *testing.T) {
	controlPlane := &fakePlatform{
		conversation: &domain.Conversation{ID: "c1", AgentVersionID: "v1"},
		snapshots:    map[string]*AgentSnapshot{"v1": {ID: "v1", SystemPrompt: "help"}},
	}
	runtime := New(controlPlane, replyChatModel{})
	var types []string
	err := runtime.ChatStream(context.Background(), ChatRequest{ConversationID: "c1", Message: "hi"}, func(event Event) error {
		types = append(types, event.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	want := []string{"run_started", "answer_delta", "answer_delta", "answer_done", "run_finished"}
	if len(types) != len(want) {
		t.Fatalf("types = %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types = %v", types)
		}
	}
}

func TestChatStreamHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := emitContext(ctx, func(Event) error { t.Fatal("must not emit"); return nil }, Event{Type: "test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
