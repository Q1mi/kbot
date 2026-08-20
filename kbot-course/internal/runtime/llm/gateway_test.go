package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/config"
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
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Content != "课堂模型回复" {
		t.Fatalf("content = %q", response.Content)
	}
}
