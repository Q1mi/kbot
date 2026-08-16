package engine

import (
	"context"

	kotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var engineTracer = otel.Tracer("kbot/runtime/engine")

// TraceOptions 控制业务内容是否进入追踪。课堂 profile 会开启，生产默认关闭。
type TraceOptions struct {
	CaptureContent bool
}

type RunStarted struct {
	ConversationID      string `json:"conversation_id"`
	AgentVersionID      string `json:"agent_version_id"`
	TraceID             string `json:"trace_id,omitempty"`
	UserMessage         string `json:"user_message,omitempty"`
	UserPromptVersionID string `json:"user_prompt_version_id,omitempty"`
}

func (e *Engine) WithTracing(options TraceOptions) *Engine {
	e.traceOptions = options
	return e
}

func (e *Engine) startChatTrace(
	ctx context.Context,
	req ChatStreamRequest,
	convID, workspaceID, agentID, agentVersionID, promptVersionID, modelProfileVersionID, experimentVariant string,
) (context.Context, trace.Span, trace.Span, RunStarted) {
	metadata := map[string]string{
		"workspace_id":     workspaceID,
		"agent_id":         agentID,
		"agent_version_id": agentVersionID,
	}
	if promptVersionID != "" {
		metadata["prompt_version_id"] = promptVersionID
	}
	if req.UserPromptVersionID != "" {
		metadata["user_prompt_version_id"] = req.UserPromptVersionID
	}
	if modelProfileVersionID != "" {
		metadata["model_profile_version_id"] = modelProfileVersionID
	}
	if experimentVariant != "" {
		metadata["experiment_variant"] = experimentVariant
	}
	ctx = kotel.WithTraceContext(ctx, kotel.TraceContext{
		TraceName: "agent-chat", UserID: req.UserID, SessionID: convID,
		Version: agentVersionID, Tags: []string{"kbot", "agent-chat"},
		Metadata: metadata, CaptureContent: e.traceOptions.CaptureContent,
	})
	attrs := kotel.LangfuseAttributes(ctx)
	parent := trace.SpanFromContext(ctx)
	parent.SetAttributes(attrs...)
	parent.SetAttributes(attribute.String("langfuse.observation.type", "span"))
	if e.traceOptions.CaptureContent {
		parent.SetAttributes(attribute.String("langfuse.observation.input", req.Message))
	}

	ctx, span := engineTracer.Start(ctx, "agent.chat", trace.WithAttributes(attrs...))
	span.SetAttributes(
		attribute.String("langfuse.observation.type", "span"),
		attribute.String("agent.id", agentID),
		attribute.String("agent.version.id", agentVersionID),
		attribute.String("conversation.id", convID),
	)
	if e.traceOptions.CaptureContent {
		span.SetAttributes(attribute.String("langfuse.observation.input", req.Message))
	}
	traceID := span.SpanContext().TraceID().String()
	return ctx, span, parent, RunStarted{
		ConversationID: convID, AgentVersionID: agentVersionID, TraceID: traceID, UserMessage: req.Message,
		UserPromptVersionID: req.UserPromptVersionID,
	}
}

func finishChatTrace(span, parent trace.Span, output string, err error, capture bool) {
	if capture && output != "" {
		attrs := []attribute.KeyValue{attribute.String("langfuse.observation.output", output)}
		span.SetAttributes(attrs...)
		parent.SetAttributes(attrs...)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func startOperationSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	all := append(kotel.LangfuseAttributes(ctx), attribute.String("langfuse.observation.type", "span"))
	all = append(all, attrs...)
	return engineTracer.Start(ctx, name, trace.WithAttributes(all...))
}

func finishOperationSpan(ctx context.Context, span trace.Span, output string, err error) {
	if kotel.CaptureContent(ctx) && output != "" {
		span.SetAttributes(attribute.String("langfuse.observation.output", output))
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
