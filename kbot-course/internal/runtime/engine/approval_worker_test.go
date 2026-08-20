package engine

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/approval"
)

type recordingResumer struct{ calls int }

func (r *recordingResumer) ResumeApproved(_ context.Context, request *approval.Request, checkpoint []byte, emit Emitter) error {
	r.calls++
	if request.ToolVersionID != "refund-v1" || string(checkpoint) != "checkpoint" {
		return context.Canceled
	}
	return nil
}

func TestApprovalWorkerClaimsResumesAndCompletes(t *testing.T) {
	service := approval.NewService()
	created, err := service.Create(t.Context(), approval.Request{
		WorkspaceID: "ws", RunID: "conversation", ToolCallID: "call", ToolVersionID: "refund-v1",
		Arguments: []byte(`{"order_id":"ORD-1"}`), Checkpoint: []byte("checkpoint"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Decide(t.Context(), "ws", created.ID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	resumer := &recordingResumer{}
	worker := NewApprovalWorker(service, resumer, "worker-1")
	if err := worker.runBatch(t.Context()); err != nil {
		t.Fatal(err)
	}
	request, err := service.Get(t.Context(), "ws", created.ID)
	if err != nil || request.Status != approval.StatusCompleted || resumer.calls != 1 {
		t.Fatalf("request = %#v, calls = %d, err = %v", request, resumer.calls, err)
	}
	if err := worker.runBatch(t.Context()); err != nil || resumer.calls != 1 {
		t.Fatalf("completed task was executed again: calls=%d err=%v", resumer.calls, err)
	}
}
