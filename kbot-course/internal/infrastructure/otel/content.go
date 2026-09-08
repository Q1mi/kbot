package otel

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ContentLimit also bounds the extra message data retained by streaming tracing.
const ContentLimit = 16 * 1024

// CaptureContent is opt-in because prompts and tool results can contain user data.
func CaptureContent() bool { return os.Getenv("KBOT_TRACE_CONTENT") == "true" }

func RecordInput(ctx context.Context, value any)  { recordContent(ctx, "input", value) }
func RecordOutput(ctx context.Context, value any) { recordContent(ctx, "output", value) }

func recordContent(ctx context.Context, field string, value any) {
	span := trace.SpanFromContext(ctx)
	if !CaptureContent() || !span.IsRecording() {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return
	}
	raw, err = json.Marshal(redact(decoded))
	if err != nil {
		return
	}
	if len(raw) > ContentLimit {
		// Keep the attribute valid JSON and UTF-8 even when the payload is truncated.
		// Re-encoding a JSON preview can double its backslashes and quotes.
		preview := raw[:ContentLimit/4]
		for !utf8.Valid(preview) {
			preview = preview[:len(preview)-1]
		}
		raw, _ = json.Marshal(map[string]any{"truncated": true, "preview": string(preview)})
		span.SetAttributes(attribute.Bool("kbot.trace."+field+".truncated", true))
	}
	span.SetAttributes(attribute.String("langfuse.observation."+field, string(raw)))
}

// Redact common structured credential fields, including JSON-encoded tool arguments.
// Free-form user text needs application-specific sanitization before capture is enabled.
func redact(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
			switch normalized {
			case "apikey", "authorization", "password", "secret", "secretkey", "token", "accesstoken", "refreshtoken", "cookie":
				v[key] = "[REDACTED]"
			default:
				v[key] = redact(item)
			}
		}
	case []any:
		for i := range v {
			v[i] = redact(v[i])
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var nested any
			decoder := json.NewDecoder(strings.NewReader(trimmed))
			decoder.UseNumber()
			if decoder.Decode(&nested) == nil && json.Valid([]byte(trimmed)) {
				raw, err := json.Marshal(redact(nested))
				if err == nil {
					return string(raw)
				}
			}
		}
	}
	return value
}
