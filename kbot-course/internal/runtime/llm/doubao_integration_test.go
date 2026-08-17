//go:build integration

package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/config"
)

func TestDoubaoGenerate(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("ARK_API_KEY"))
	if apiKey == "" {
		t.Fatal("ARK_API_KEY is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("KBOT_LLM_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}
	modelName := strings.TrimSpace(os.Getenv("KBOT_LLM_MODEL"))
	if modelName == "" {
		modelName = "doubao-seed-2-0-lite-260215"
	}

	gateway, err := NewGateway(config.Config{
		LLMBaseURL: baseURL,
		LLMAPIKey:  apiKey,
		LLMModel:   modelName,
		LLMTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := gateway.Generate(ctx, []*schema.Message{
		schema.UserMessage("请用一句话介绍你自己。"),
	}, nil)
	if err != nil {
		t.Fatalf("generate with Doubao: %v", err)
	}
	if strings.TrimSpace(response.Content) == "" {
		t.Fatal("Doubao returned empty content")
	}
	t.Logf("model=%s response=%s", modelName, response.Content)
}
