package llm

import (
	"context"

	kotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("kbot/runtime/llm")

// callResult 携带 GenAI 语义约定关心的指标。
type callResult struct {
	inputTokens  int
	outputTokens int
	cachedTokens int // Prompt Cache 命中量(usage.cached_tokens)
	finishReason string
	input        string
	output       string
}

// withSpan 把一次模型调用包到 OpenTelemetry GenAI 语义约定的 span 里（讲义 §15.2）。
// 用业界共识属性名（gen_ai.*），Langfuse / Phoenix / Arize 开箱即用、能聚合 token 成本。
func withSpan(ctx context.Context, provider, model string, do func(ctx context.Context) (callResult, error)) (callResult, error) {
	attrs := append(kotel.LangfuseAttributes(ctx),
		attribute.String("gen_ai.system", provider),
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.request.model", model),
		attribute.String("langfuse.observation.type", "generation"),
	)
	ctx, span := tracer.Start(ctx, "chat "+model, trace.WithAttributes(attrs...))
	defer span.End()

	res, err := do(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return res, err
	}
	span.SetAttributes(
		attribute.Int("gen_ai.usage.input_tokens", res.inputTokens),
		attribute.Int("gen_ai.usage.output_tokens", res.outputTokens),
		attribute.Int("gen_ai.usage.cached_tokens", res.cachedTokens),
	)
	if res.finishReason != "" {
		span.SetAttributes(attribute.String("gen_ai.response.finish_reasons", res.finishReason))
	}
	if kotel.CaptureContent(ctx) {
		span.SetAttributes(
			attribute.String("langfuse.observation.input", res.input),
			attribute.String("langfuse.observation.output", res.output),
		)
	}
	return res, nil
}
