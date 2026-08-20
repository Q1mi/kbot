package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/llm"
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

func TestChatStreamResolvesEinoPromptAndPinnedModelPlan(t *testing.T) {
	var requestedModel, systemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role, Content string
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requestedModel = request.Model
		if len(request.Messages) > 0 {
			systemPrompt = request.Messages[0].Content
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-course", "object": "chat.completion", "created": 1, "model": request.Model,
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "profile reply"}, "finish_reason": "stop"}},
		})
	}))
	defer server.Close()

	gateway, err := llm.NewGateway(config.Config{
		LLMBaseURL: server.URL + "/v1", LLMAPIKey: "fallback", LLMModel: "fallback", LLMTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	prompts := prompt.NewService()
	if err := prompts.Publish(t.Context(), prompt.Version{
		ID: "prompt-v1", WorkspaceID: "ws-1", Name: "system", Template: "Pinned system prompt",
	}); err != nil {
		t.Fatal(err)
	}
	profiles := modelconfig.NewRegistry()
	if err := profiles.Publish(t.Context(), modelconfig.ProfileVersion{
		ID: "profile-v1", WorkspaceID: "ws-1", ClassificationMax: "internal",
		Deployments: []modelconfig.Deployment{{
			Provider: "doubao", Model: "doubao-course", BaseURL: server.URL + "/v1", APIKey: "course-key", MaxRetries: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	controlPlane := &fakePlatform{
		conversation: &domain.Conversation{ID: "c1", WorkspaceID: "ws-1", AgentVersionID: "v1"},
		snapshots: map[string]*AgentSnapshot{"v1": {
			ID: "v1", WorkspaceID: "ws-1", MaxSteps: 4,
			PromptVersionID: "prompt-v1", ModelProfileVersionID: "profile-v1",
		}},
	}
	runtime := New(controlPlane, gateway).WithRuntimeConfig(prompts, profiles)
	if err := runtime.ChatStream(t.Context(), ChatRequest{
		ConversationID: "c1", WorkspaceID: "ws-1", Message: "hi",
	}, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if requestedModel != "doubao-course" || systemPrompt != "Pinned system prompt" {
		t.Fatalf("model=%q system=%q", requestedModel, systemPrompt)
	}
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
