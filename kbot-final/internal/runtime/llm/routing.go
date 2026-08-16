package llm

import (
	"context"

	"github.com/Q1mi/kbot/internal/domain"
)

// 数据分级随 context 传到网关并驱动 Provider 路由。
// 引擎在对话开始时把会话的 classification 放进 ctx,网关 Generate 时读出来选 Provider。

type classificationKey struct{}
type invocationKey struct{}

// WithClassification 把数据分级写入 context。
func WithClassification(ctx context.Context, classification string) context.Context {
	if classification == "" {
		return ctx
	}
	return context.WithValue(ctx, classificationKey{}, classification)
}

// classificationFromContext 读出数据分级(无则空)。
func classificationFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(classificationKey{}).(string); ok {
		return v
	}
	return ""
}

// InvocationConfig 是一次会话已固化的 Prompt/模型配置与实验归因。
type InvocationConfig struct {
	WorkspaceID           string
	Environment           string
	AgentID               string
	UserID                string
	PromptVersionID       string
	ModelProfileVersionID string
	GenerationConfig      domain.GenerationConfig
	ExperimentID          string
	ExperimentVariant     string
}

func WithInvocationConfig(ctx context.Context, cfg InvocationConfig) context.Context {
	return context.WithValue(ctx, invocationKey{}, cfg)
}

func invocationFromContext(ctx context.Context) InvocationConfig {
	v, _ := ctx.Value(invocationKey{}).(InvocationConfig)
	return v
}
