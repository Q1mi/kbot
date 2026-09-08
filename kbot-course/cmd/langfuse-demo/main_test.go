package main

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	globalotel "go.opentelemetry.io/otel"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Run the student-facing demo through the real Engine and HTTP exporter.
func TestDemoExportsCompleteAgentTraces(t *testing.T) {
	var mu sync.Mutex
	var spans []*tracepb.Span
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		mu.Lock()
		for _, resource := range payload.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+"/api/public/otel/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "")
	t.Setenv("KBOT_TRACE_CONTENT", "true")
	previous := globalotel.GetTracerProvider()
	defer globalotel.SetTracerProvider(previous)
	if err := run(false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(spans) != 8 {
		t.Fatalf("got %d spans, want 8", len(spans))
	}
	roots := map[string]string{}
	counts := map[string]int{}
	errors := 0
	usageCount := 0
	sessions := map[string]int{}
	for _, span := range spans {
		attrs := map[string]string{}
		for _, a := range span.Attributes {
			attrs[a.Key] = a.Value.GetStringValue()
		}
		counts[span.Name]++
		if span.Name == "agent.run" {
			roots[hex.EncodeToString(span.TraceId)] = hex.EncodeToString(span.SpanId)
			sessions[attrs["langfuse.session.id"]]++
		}
		if attrs["langfuse.user.id"] != "demo-user" || attrs["langfuse.trace.metadata.workspace_id"] != "demo-workspace" {
			t.Fatalf("missing propagated attributes on %s", span.Name)
		}
		if span.EndTimeUnixNano < span.StartTimeUnixNano {
			t.Fatal("invalid span timestamps")
		}
		if span.Status.GetCode() == tracepb.Status_STATUS_CODE_ERROR {
			errors++
		}
		if attrs["langfuse.observation.type"] == "generation" {
			if attrs["langfuse.observation.model.name"] != "course-demo-model" {
				t.Fatal("missing model name")
			}
			if attrs["langfuse.observation.usage_details"] != "" {
				usageCount++
			}
		}
		if attrs["langfuse.observation.input"] == "" || attrs["langfuse.observation.output"] == "" && span.Status.GetCode() != tracepb.Status_STATUS_CODE_ERROR {
			t.Fatalf("missing content on %s", span.Name)
		}
	}
	if counts["agent.run"] != 3 || counts["llm.stream"] != 2 || counts["llm.generate"] != 2 || counts["tool.execute"] != 1 || errors != 2 || usageCount != 3 {
		t.Fatalf("counts=%v errors=%d usage=%d", counts, errors, usageCount)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions=%v", sessions)
	}
	for _, span := range spans {
		if span.Name != "agent.run" && hex.EncodeToString(span.ParentSpanId) != roots[hex.EncodeToString(span.TraceId)] {
			t.Fatalf("orphaned span %s", span.Name)
		}
	}
}
