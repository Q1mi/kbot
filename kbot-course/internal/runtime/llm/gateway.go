// Package llm 负责构造 Eino 标准 ChatModel 与 ADK 模型执行策略。
package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
)

// ExecutionPlan 把主模型、重试策略和部署故障切换策略交给 Eino ADK。
type ExecutionPlan struct {
	Model    model.BaseChatModel
	Retry    *adk.ModelRetryConfig
	Failover *adk.ModelFailoverConfig[*schema.Message]
}

// Gateway 保留 Provider 构造边界，同时直接实现 Eino model.BaseChatModel。
type Gateway struct {
	model   model.BaseChatModel
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
	ctx context.Context, messages []*schema.Message, opts ...model.Option,
) (*schema.Message, error) {
	if g == nil || g.model == nil {
		return nil, fmt.Errorf("chat model is required")
	}
	response, err := g.model.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, fmt.Errorf("generate response: %w", err)
	}
	return response, nil
}

func (g *Gateway) Stream(
	ctx context.Context, messages []*schema.Message, opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	if g == nil || g.model == nil {
		return nil, fmt.Errorf("chat model is required")
	}
	stream, err := g.model.Stream(ctx, messages, opts...)
	if err != nil {
		return nil, fmt.Errorf("stream response: %w", err)
	}
	return stream, nil
}

// PrepareExecution 将固定 Model Profile 转成 Eino ADK 原生重试与故障切换配置。
func (g *Gateway) PrepareExecution(ctx context.Context, profile modelconfig.ProfileVersion) (*ExecutionPlan, error) {
	if g == nil || len(profile.Deployments) == 0 {
		return nil, fmt.Errorf("model profile has no deployments")
	}
	models := make([]model.BaseChatModel, 0, len(profile.Deployments))
	maxRetries := 0
	for index, deployment := range profile.Deployments {
		if !supportedProvider(deployment.Provider) {
			return nil, fmt.Errorf("deployment %d: unsupported provider %q", index, deployment.Provider)
		}
		selected, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey: deployment.APIKey, BaseURL: deployment.BaseURL, Model: deployment.Model, Timeout: g.timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("deployment %d: %w", index, err)
		}
		models = append(models, selected)
		if deployment.MaxRetries > maxRetries {
			maxRetries = deployment.MaxRetries
		}
	}
	plan := &ExecutionPlan{Model: models[0]}
	if maxRetries > 0 {
		plan.Retry = &adk.ModelRetryConfig{MaxRetries: maxRetries}
	}
	if len(models) > 1 {
		plan.Failover = &adk.ModelFailoverConfig[*schema.Message]{
			MaxRetries: uint(len(models) - 1),
			ShouldFailover: func(_ context.Context, _ *schema.Message, callErr error) bool {
				return callErr != nil
			},
			GetFailoverModel: func(_ context.Context, failover *adk.FailoverContext[*schema.Message]) (model.BaseChatModel, []*schema.Message, error) {
				index := int(failover.FailoverAttempt)
				if index <= 0 || index >= len(models) {
					return nil, nil, fmt.Errorf("model failover attempt %d is out of range", index)
				}
				return models[index], nil, nil
			},
		}
	}
	return plan, nil
}

func supportedProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai-compatible", "openai", "deepseek", "doubao":
		return true
	default:
		return false
	}
}
