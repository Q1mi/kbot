package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
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
				"index": 0, "message": map[string]any{"role": "assistant", "content": "课堂模型回复"}, "finish_reason": "stop",
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
	response, err := gateway.Generate(context.Background(), []*schema.Message{schema.UserMessage("你好")})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Content != "课堂模型回复" {
		t.Fatalf("content = %q", response.Content)
	}
}

func TestPrepareExecutionBuildsEinoRetryAndFailover(t *testing.T) {
	gateway, err := NewGateway(config.Config{
		LLMBaseURL: "https://fallback.example/v1", LLMAPIKey: "fallback", LLMModel: "fallback", LLMTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := gateway.PrepareExecution(t.Context(), modelconfig.ProfileVersion{
		Deployments: []modelconfig.Deployment{
			{Provider: "deepseek", Model: "primary", BaseURL: "https://primary.example/v1", APIKey: "primary-key", MaxRetries: 1},
			{Provider: "doubao", Model: "backup", BaseURL: "https://backup.example/v1", APIKey: "backup-key", MaxRetries: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Model == nil || plan.Retry == nil || plan.Retry.MaxRetries != 3 {
		t.Fatalf("execution plan = %+v", plan)
	}
	if plan.Failover == nil || plan.Failover.MaxRetries != 1 {
		t.Fatalf("failover config = %+v", plan.Failover)
	}
	backup, replacement, err := plan.Failover.GetFailoverModel(t.Context(), &adk.FailoverContext[*schema.Message]{FailoverAttempt: 1})
	if err != nil || backup == nil || replacement != nil {
		t.Fatalf("backup=%v replacement=%v err=%v", backup, replacement, err)
	}
}
