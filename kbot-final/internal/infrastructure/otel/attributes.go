package otel

import (
	"context"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

type traceContextKey struct{}

// TraceContext 是一次 Agent 运行需要传播到所有 Langfuse Observation 的公共维度。
// Metadata 只放可筛选的短字符串，避免把密钥或大段业务内容写进属性。
type TraceContext struct {
	TraceName      string
	UserID         string
	SessionID      string
	Version        string
	Tags           []string
	Metadata       map[string]string
	CaptureContent bool
}

func WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	tc.SessionID = normalizeUUIDSessionID(tc.SessionID)
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// normalizeUUIDSessionID 统一运行时 compact UUID 与 PostgreSQL canonical UUID。
// Langfuse 按 session ID 的精确字符串分组，异步 server/worker 必须使用同一格式。
func normalizeUUIDSessionID(value string) string {
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return value
	}
	for _, char := range compact {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return value
		}
	}
	return strings.ToLower(compact)
}

func TraceContextFrom(ctx context.Context) TraceContext {
	if tc, ok := ctx.Value(traceContextKey{}).(TraceContext); ok {
		return tc
	}
	return TraceContext{}
}

// LangfuseAttributes 返回每个手工 Span 都应携带的属性。
// Langfuse v4 以 Observation 为中心，公共维度需要出现在可聚合的子 Span 上。
func LangfuseAttributes(ctx context.Context) []attribute.KeyValue {
	tc := TraceContextFrom(ctx)
	attrs := make([]attribute.KeyValue, 0, 6+len(tc.Metadata))
	if tc.TraceName != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.name", tc.TraceName))
	}
	if tc.UserID != "" {
		attrs = append(attrs, attribute.String("langfuse.user.id", tc.UserID))
	}
	if tc.SessionID != "" {
		attrs = append(attrs, attribute.String("langfuse.session.id", tc.SessionID))
	}
	if tc.Version != "" {
		attrs = append(attrs, attribute.String("langfuse.version", tc.Version))
	}
	if len(tc.Tags) > 0 {
		attrs = append(attrs, attribute.StringSlice("langfuse.trace.tags", tc.Tags))
	}
	keys := make([]string, 0, len(tc.Metadata))
	for key := range tc.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata."+key, tc.Metadata[key]))
	}
	return attrs
}

func CaptureContent(ctx context.Context) bool {
	return TraceContextFrom(ctx).CaptureContent
}
