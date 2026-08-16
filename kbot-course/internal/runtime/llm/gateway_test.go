package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
)

func TestGatewayGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "created": 1, "model": "demo",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "课堂模型回复"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	gateway, err := NewGateway(config.Config{
		LLMBaseURL: server.URL + "/v1", LLMAPIKey: "test", LLMModel: "demo", LLMTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}
	response, err := gateway.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("你好"),
	}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Content != "课堂模型回复" {
		t.Fatalf("content = %q", response.Content)
	}
}

func TestGatewayUsesPinnedProfileAndFallsBackInOrder(t *testing.T) {
	var primaryCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "primary unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secondary-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "doubao-course" {
			t.Fatalf("model = %q", request.Model)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-profile", "object": "chat.completion", "created": 1, "model": request.Model,
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "profile reply"}, "finish_reason": "stop"}},
		})
	}))
	defer secondary.Close()

	gateway, err := NewGateway(config.Config{LLMBaseURL: secondary.URL + "/v1", LLMAPIKey: "fallback", LLMModel: "fallback", LLMTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	response, err := gateway.GenerateWithProfile(t.Context(), modelconfig.ProfileVersion{Deployments: []modelconfig.Deployment{
		{Provider: "deepseek", Model: "primary", BaseURL: primary.URL + "/v1", APIKey: "primary-key"},
		{Provider: "doubao", Model: "doubao-course", BaseURL: secondary.URL + "/v1", APIKey: "secondary-key"},
	}}, []*schema.Message{schema.UserMessage("hi")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if primaryCalls.Load() == 0 || response.Content != "profile reply" {
		t.Fatalf("primary calls = %d, response = %#v", primaryCalls.Load(), response)
	}
}
