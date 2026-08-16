// Package llm 把模型 SDK 收敛为 Runtime 使用的稳定接口。
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
)

// Gateway 是 Eino ChatModel 与 Agent Runtime 之间的防腐层。
// 上层只依赖 Generate，后续更换 Provider 时无需改 Agent 循环。
type Gateway struct {
	model   model.ToolCallingChatModel
	timeout time.Duration
}

func NewGateway(cfg config.Config) (*Gateway, error) {
	chatModel, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		APIKey:  cfg.LLMAPIKey,
		BaseURL: cfg.LLMBaseURL,
		Model:   cfg.LLMModel,
		Timeout: cfg.LLMTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create chat model: %w", err)
	}
	return &Gateway{model: chatModel, timeout: cfg.LLMTimeout}, nil
}

func (g *Gateway) Generate(
	ctx context.Context,
	messages []*schema.Message,
	tools []*schema.ToolInfo,
) (*schema.Message, error) {
	if g == nil || g.model == nil {
		return nil, fmt.Errorf("chat model is required")
	}
	return generate(ctx, g.model, messages, tools)
}

// GenerateWithProfile 严格按照固定 Profile Version 的主备顺序调用 OpenAI 兼容部署。
func (g *Gateway) GenerateWithProfile(ctx context.Context, profile modelconfig.ProfileVersion, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	if g == nil || len(profile.Deployments) == 0 {
		return nil, fmt.Errorf("model profile has no deployments")
	}
	var failures []error
	for index, deployment := range profile.Deployments {
		provider := strings.ToLower(strings.TrimSpace(deployment.Provider))
		switch provider {
		case "openai-compatible", "openai", "deepseek", "doubao":
		default:
			failures = append(failures, fmt.Errorf("deployment %d: unsupported provider %q", index, deployment.Provider))
			continue
		}
		selected, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey: deployment.APIKey, BaseURL: deployment.BaseURL, Model: deployment.Model, Timeout: g.timeout,
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("deployment %d: %w", index, err))
			continue
		}
		response, err := generate(ctx, selected, messages, tools)
		if err == nil {
			return response, nil
		}
		failures = append(failures, fmt.Errorf("deployment %d: %w", index, err))
	}
	return nil, fmt.Errorf("all deployments failed: %w", errors.Join(failures...))
}

func generate(ctx context.Context, selected model.ToolCallingChatModel, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	if len(tools) > 0 {
		bound, err := selected.WithTools(tools)
		if err != nil {
			return nil, fmt.Errorf("bind tools: %w", err)
		}
		selected = bound
	}
	response, err := selected.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("generate response: %w", err)
	}
	return response, nil
}
