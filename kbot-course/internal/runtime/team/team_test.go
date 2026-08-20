package team

import (
	"context"
	"testing"
)

type fakeWorker struct{ version string }

func (w *fakeWorker) Run(_ context.Context, version, task string) (string, error) {
	w.version = version
	return task + " done", nil
}

func TestCoordinatorDispatchesPinnedMemberVersion(t *testing.T) {
	worker := &fakeWorker{}
	coordinator, err := NewCoordinator([]Member{{Name: "researcher", AgentVersionID: "research-v2"}}, worker)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := coordinator.Dispatch(context.Background(), "researcher", "collect evidence")
	if err != nil || answer != "collect evidence done" || worker.version != "research-v2" {
		t.Fatalf("answer=%q version=%q err=%v", answer, worker.version, err)
	}
}
