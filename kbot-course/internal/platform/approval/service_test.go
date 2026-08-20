package approval

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestApprovalBindsCheckpointToExactToolCall(t *testing.T) {
	service := NewService()
	request, err := service.Create(context.Background(), Request{WorkspaceID: "ws", RunID: "run-1", ToolCallID: "call-1", ToolVersionID: "refund-v1", Arguments: []byte(`{"amount":299,"order_id":"ORD-1"}`), Checkpoint: []byte("checkpoint")})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Decide(context.Background(), "ws", request.ID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := service.Resume(context.Background(), "ws", request.ID, "run-1", "call-1", "refund-v1", []byte(`{"order_id":"ORD-1","amount":299}`))
	if err != nil || string(checkpoint) != "checkpoint" {
		t.Fatalf("checkpoint=%q err=%v", checkpoint, err)
	}
	if _, err := service.Resume(context.Background(), "ws", request.ID, "run-1", "call-1", "refund-v1", request.Arguments); err == nil {
		t.Fatal("expected one-time consumption")
	}
}

func TestApprovalRejectsTamperedArgumentsAndExpiredDecision(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	request, _ := service.Create(context.Background(), Request{WorkspaceID: "ws", RunID: "run", ToolCallID: "call", ToolVersionID: "v1", Arguments: []byte(`{"amount":100}`), ExpiresAt: now.Add(time.Minute)})
	_ = service.Decide(context.Background(), "ws", request.ID, "reviewer", true)
	if _, err := service.Resume(context.Background(), "ws", request.ID, "run", "call", "v1", []byte(`{"amount":1000}`)); err == nil {
		t.Fatal("expected tampered arguments to fail")
	}
	request2, _ := service.Create(context.Background(), Request{WorkspaceID: "ws", RunID: "run-2", ToolCallID: "call-2", ToolVersionID: "v1", Arguments: []byte(`{}`), ExpiresAt: now.Add(time.Minute)})
	now = now.Add(2 * time.Minute)
	if err := service.Decide(context.Background(), "ws", request2.ID, "reviewer", true); err == nil {
		t.Fatal("expected expired approval to fail")
	}
}

func TestResumeBindsToolIdentityAndPreservesLargeJSONIntegers(t *testing.T) {
	service := NewService()
	created, err := service.Create(t.Context(), Request{
		WorkspaceID: "ws", RunID: "run-1", ToolCallID: "call-1", ToolVersionID: "transfer-v3",
		Arguments: []byte(`{"account_id":9007199254740992}`), Checkpoint: []byte(`{"step":2}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Decide(t.Context(), "ws", created.ID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, call, version string
		arguments           []byte
	}{
		{name: "tool call", call: "call-2", version: "transfer-v3", arguments: []byte(`{"account_id":9007199254740992}`)},
		{name: "tool version", call: "call-1", version: "transfer-v4", arguments: []byte(`{"account_id":9007199254740992}`)},
		{name: "large integer", call: "call-1", version: "transfer-v3", arguments: []byte(`{"account_id":9007199254740993}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Resume(t.Context(), "ws", created.ID, "run-1", test.call, test.version, test.arguments); err == nil {
				t.Fatal("mismatched approval binding was accepted")
			}
		})
	}
}

func TestExecutionLeaseUsesFencingAndBoundedRetry(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, err := service.Create(t.Context(), Request{WorkspaceID: "ws", RunID: "run", ToolCallID: "call", ToolVersionID: "v1", Arguments: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Decide(t.Context(), "ws", created.ID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	first, err := service.ClaimExecution(t.Context(), "ws", created.ID, "run", "call", "v1", "worker-1", []byte(`{}`), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	second, err := service.ClaimExecution(t.Context(), "ws", created.ID, "run", "call", "v1", "worker-2", []byte(`{}`), time.Minute)
	if err != nil || second.Token <= first.Token {
		t.Fatalf("second lease = %#v, %v", second, err)
	}
	if err := service.Complete(t.Context(), "ws", created.ID, first.Token); err == nil {
		t.Fatal("stale worker completed a newer lease")
	}
	if err := service.Fail(t.Context(), "ws", created.ID, second.Token, fmt.Errorf("temporary"), 3); err != nil {
		t.Fatal(err)
	}
	third, err := service.ClaimExecution(t.Context(), "ws", created.ID, "run", "call", "v1", "worker-3", []byte(`{}`), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Complete(t.Context(), "ws", created.ID, third.Token); err != nil {
		t.Fatal(err)
	}
	request, _ := service.Get(t.Context(), "ws", created.ID)
	if request.Status != StatusCompleted || request.Attempts != 3 {
		t.Fatalf("request = %#v", request)
	}
}
