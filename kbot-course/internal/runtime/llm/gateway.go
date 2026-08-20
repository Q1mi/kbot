// Package llm 负责构造 Eino 标准 ChatModel。
package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/config"
)

// Gateway 保留 Provider 构造边界，同时直接实现 Eino model.BaseChatModel。
type Gateway struct {
	model model.BaseChatModel
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
	return &Gateway{model: chatModel}, nil
}

func (g *Gateway) Generate(
	ctx context.Context,
	messages []*schema.Message,
	opts ...model.Option,
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
