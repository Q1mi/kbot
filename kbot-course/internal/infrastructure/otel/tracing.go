// Package otel 统一 Agent 运行的 OpenTelemetry 与 Langfuse 属性。
package otel

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	globalotel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type RunContext struct{ WorkspaceID, AgentVersionID, ConversationID, UserID, Environment string }

type runKey struct{}
type runState struct {
	attributes []attribute.KeyValue
	mu         sync.Mutex
	cleanups   []func(error)
	ended      bool
	err        error
}

// OnRunFinish closes observations left open when a consumer stops reading a stream.
// Callbacks run synchronously before the root span ends.
func OnRunFinish(ctx context.Context, cleanup func(error)) {
	if state, ok := ctx.Value(runKey{}).(*runState); ok {
		state.mu.Lock()
		if !state.ended {
			state.cleanups = append(state.cleanups, cleanup)
			state.mu.Unlock()
			return
		}
		err := state.err
		state.mu.Unlock()
		cleanup(err)
	}
}

// Setup 在配置 OTLP endpoint 时安装批量导出的 SDK；Langfuse 可直接接收 OTLP/HTTP traces。
func Setup(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" && strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")) == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	globalotel.SetTracerProvider(provider)
	return func(shutdownCtx context.Context) error {
		return errors.Join(provider.ForceFlush(shutdownCtx), provider.Shutdown(shutdownCtx))
	}, nil
}

func Attributes(run RunContext) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("kbot.workspace.id", run.WorkspaceID),
		attribute.String("kbot.agent.version.id", run.AgentVersionID),
		attribute.String("kbot.conversation.id", run.ConversationID),
		attribute.String("langfuse.trace.name", "agent.run"),
		attribute.String("langfuse.session.id", run.ConversationID),
		attribute.String("langfuse.user.id", run.UserID),
		attribute.String("langfuse.version", run.AgentVersionID),
		attribute.String("langfuse.environment", environment(run.Environment)),
		attribute.String("langfuse.trace.metadata.workspace_id", run.WorkspaceID),
		attribute.String("langfuse.trace.metadata.agent_version_id", run.AgentVersionID),
	}
}

func StartRun(ctx context.Context, run RunContext) (context.Context, func(error)) {
	state := &runState{attributes: Attributes(run)}
	ctx = context.WithValue(ctx, runKey{}, state)
	ctx, finish := StartOperation(ctx, "agent.run", attribute.String("langfuse.observation.type", "agent"))
	var once sync.Once
	return ctx, func(err error) {
		once.Do(func() {
			state.mu.Lock()
			state.ended, state.err = true, err
			cleanups := state.cleanups
			state.cleanups = nil
			state.mu.Unlock()
			for _, cleanup := range cleanups {
				cleanup(err)
			}
			finish(err)
		})
	}
}

func StartOperation(ctx context.Context, name string, attributes ...attribute.KeyValue) (context.Context, func(error)) {
	if state, ok := ctx.Value(runKey{}).(*runState); ok {
		attributes = append(append([]attribute.KeyValue(nil), state.attributes...), attributes...)
	}
	ctx, span := globalotel.Tracer("github.com/Q1mi/kbot/runtime").Start(ctx, name, trace.WithAttributes(attributes...))
	var once sync.Once
	return ctx, func(err error) {
		once.Do(func() {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "completed")
			}
			span.End()
		})
	}
}

func environment(value string) string {
	if value == "" {
		value = strings.TrimSpace(os.Getenv("KBOT_TRACE_ENVIRONMENT"))
	}
	if value == "" {
		value = "development"
	}
	return value
}
