// Package otel 统一 Agent 运行的 OpenTelemetry 与 Langfuse 属性。
package otel

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("tracing is implemented in 19-end")

type RunContext struct{ WorkspaceID, AgentVersionID, ConversationID, UserID string }

func StartRun(context.Context, RunContext) (context.Context, func(error)) {
	return context.Background(), func(error) {}
}
