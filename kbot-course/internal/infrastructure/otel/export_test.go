package otel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	globalotel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPExportToLangfuseEndpoint(t *testing.T) {
	for _, signalSpecific := range []bool{false, true} {
		t.Run(map[bool]string{true: "traces_endpoint", false: "base_endpoint"}[signalSpecific], func(t *testing.T) {
			requests := make(chan *collector.ExportTraceServiceRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/public/otel/v1/traces" || r.Header.Get("Authorization") != "Basic test-auth" || r.Header.Get("x-langfuse-ingestion-version") != "4" {
					t.Errorf("unexpected OTLP route or auth headers: %s", r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/x-protobuf" {
					t.Error("expected protobuf transport")
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				payload := new(collector.ExportTraceServiceRequest)
				if err := proto.Unmarshal(raw, payload); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				requests <- payload
				w.Header().Set("Content-Type", "application/x-protobuf")
				_, _ = w.Write([]byte{})
			}))
			defer server.Close()
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "")
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic test-auth,x-langfuse-ingestion-version=4")
			if signalSpecific {
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+"/api/public/otel/v1/traces")
			} else {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL+"/api/public/otel")
			}
			previous := globalotel.GetTracerProvider()
			defer globalotel.SetTracerProvider(previous)
			shutdown, err := Setup(t.Context(), "course-19-test")
			if err != nil {
				t.Fatal(err)
			}
			ctx, endRun := StartRun(t.Context(), RunContext{WorkspaceID: "ws", UserID: "u", ConversationID: "c", AgentVersionID: "v"})
			_, endTool := StartOperation(ctx, "tool.execute", attribute.String("langfuse.observation.type", "tool"))
			endTool(nil)
			endRun(nil)
			if err := shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case payload := <-requests:
				count := 0
				for _, resource := range payload.ResourceSpans {
					for _, scope := range resource.ScopeSpans {
						count += len(scope.Spans)
					}
				}
				if count != 2 {
					t.Fatalf("exported %d spans", count)
				}
			default:
				t.Fatal("no OTLP request received after shutdown")
			}
		})
	}
}

func TestContentCaptureRedactsAndBoundsJSON(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := globalotel.GetTracerProvider()
	globalotel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()); globalotel.SetTracerProvider(previous) })
	t.Setenv("KBOT_TRACE_CONTENT", "false")
	ctx, end := StartRun(t.Context(), RunContext{})
	RecordInput(ctx, "private input")
	t.Setenv("KBOT_TRACE_CONTENT", "true")
	RecordOutput(ctx, map[string]any{"password": "do-not-export", "arguments": `{"api_key":"nested-secret","id":9007199254740993}`})
	end(nil)
	attrs := map[string]string{}
	for _, attr := range recorder.Ended()[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	if _, ok := attrs["langfuse.observation.input"]; ok {
		t.Fatal("content captured while disabled")
	}
	output := attrs["langfuse.observation.output"]
	if !json.Valid([]byte(output)) || strings.Contains(output, "do-not-export") || strings.Contains(output, "nested-secret") || !strings.Contains(output, "9007199254740993") {
		t.Fatalf("redacted output: %s", output)
	}
	ctx, end = StartRun(t.Context(), RunContext{})
	RecordOutput(ctx, strings.Repeat("课", ContentLimit))
	end(nil)
	for _, attr := range recorder.Ended()[1].Attributes() {
		if attr.Key == "langfuse.observation.output" {
			value := attr.Value.AsString()
			if len(value) > ContentLimit || !json.Valid([]byte(value)) || !strings.Contains(value, "truncated") {
				t.Fatal("invalid bounded JSON")
			}
		}
	}
}
