package otel

import (
	"context"
	"testing"
)

func TestParseHeadersPreservesBase64Padding(t *testing.T) {
	got, err := parseHeaders("Authorization=Basic cGs6c2s=,x-langfuse-ingestion-version=4")
	if err != nil {
		t.Fatal(err)
	}
	if got["Authorization"] != "Basic cGs6c2s=" {
		t.Fatalf("authorization padding lost: %q", got["Authorization"])
	}
	if got["x-langfuse-ingestion-version"] != "4" {
		t.Fatalf("unexpected ingestion version: %q", got["x-langfuse-ingestion-version"])
	}
}

func TestParseHeadersRejectsInvalidItem(t *testing.T) {
	if _, err := parseHeaders("Authorization"); err == nil {
		t.Fatal("expected invalid header error")
	}
}

func TestWithTraceContextNormalizesUUIDSessionID(t *testing.T) {
	const compact = "dd84b0d64bdfcbd46ebb4708b9b013a0"
	ctx := WithTraceContext(context.Background(), TraceContext{
		SessionID: "DD84B0D6-4BDF-CBD4-6EBB-4708B9B013A0",
	})
	if got := TraceContextFrom(ctx).SessionID; got != compact {
		t.Fatalf("session ID = %q, want %q", got, compact)
	}
}

func TestWithTraceContextPreservesNonUUIDSessionID(t *testing.T) {
	const sessionID = "support-session-2026"
	ctx := WithTraceContext(context.Background(), TraceContext{SessionID: sessionID})
	if got := TraceContextFrom(ctx).SessionID; got != sessionID {
		t.Fatalf("session ID = %q, want %q", got, sessionID)
	}
}
