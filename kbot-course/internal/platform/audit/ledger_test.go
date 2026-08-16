package audit

import (
	"context"
	"testing"
)

func TestLedgerBuildsVerifiableWorkspaceChain(t *testing.T) {
	ledger := NewLedger()
	first, err := ledger.Append(context.Background(), Event{WorkspaceID: "ws", ActorID: "user", Action: "agent.promote", ResourceID: "agent-v1", Data: map[string]any{"env": "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.Append(context.Background(), Event{WorkspaceID: "ws", ActorID: "reviewer", Action: "approval.approve", ResourceID: "approval-1"})
	if err != nil {
		t.Fatal(err)
	}
	if second.PreviousHash != first.Hash {
		t.Fatal("events are not chained")
	}
	if err := ledger.Verify(context.Background(), "ws"); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerDetectsTampering(t *testing.T) {
	ledger := NewLedger()
	_, _ = ledger.Append(context.Background(), Event{WorkspaceID: "ws", ActorID: "user", Action: "tool.execute", ResourceID: "call-1"})
	ledger.events["ws"][0].Action = "tampered"
	if err := ledger.Verify(context.Background(), "ws"); err == nil {
		t.Fatal("expected tampering to be detected")
	}
}
