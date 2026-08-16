package approval_test

// Approval Store 契约测试:memory 与 postgres 跑同一组用例。
// convID 由工厂保证有效(PG 需 checkpoints.conversation_id 外键存在的会话;memory 不关心)。

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/util"
)

// jsonEq 比较两段 JSON 是否语义相等(JSONB 会规范化空白,故不能逐字节比)。
func jsonEq(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func runApprovalContract(t *testing.T, newStore func(t *testing.T) (approval.Store, string, string)) {
	t.Run("ApprovalLifecycle", func(t *testing.T) {
		s, convID, workspaceID := newStore(t)
		ctx := context.Background()
		a := &approval.Approval{ID: util.GenerateID(), WorkspaceID: workspaceID, ConversationID: convID, Action: "refund_money", Payload: `{"amount":100}`}
		if err := s.CreatePending(ctx, a); err != nil {
			t.Fatalf("CreatePending: %v", err)
		}
		got, err := s.Get(ctx, a.ID)
		if err != nil || got.Status != approval.StatusPending || got.Action != "refund_money" || !jsonEq(got.Payload, `{"amount":100}`) {
			t.Fatalf("Get mismatch: %+v err=%v", got, err)
		}
		pend, err := s.ListPending(ctx, workspaceID)
		if err != nil || len(pend) != 1 {
			t.Fatalf("ListPending: %v len=%d", err, len(pend))
		}
		byConversation, err := s.ListByConversation(ctx, convID)
		if err != nil || len(byConversation) != 1 || byConversation[0].ID != a.ID {
			t.Fatalf("ListByConversation: %+v err=%v", byConversation, err)
		}
		if _, err := s.ResolvePending(ctx, a.ID, workspaceID, approval.StatusApproved, util.GenerateID()); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		got, _ = s.Get(ctx, a.ID)
		if got.Status != approval.StatusApproved || got.ResolvedAt == nil {
			t.Fatalf("after resolve: %+v", got)
		}
		pend, _ = s.ListPending(ctx, workspaceID)
		if len(pend) != 0 {
			t.Fatalf("expected no pending after resolve, got %d", len(pend))
		}
		byConversation, err = s.ListByConversation(ctx, convID)
		if err != nil || len(byConversation) != 1 || byConversation[0].Status != approval.StatusApproved {
			t.Fatalf("resolved ListByConversation: %+v err=%v", byConversation, err)
		}
	})

	t.Run("MissingApproval", func(t *testing.T) {
		s, _, _ := newStore(t)
		if _, err := s.Get(context.Background(), util.GenerateID()); err == nil {
			t.Fatal("expected error for missing approval")
		}
	})

	t.Run("Checkpoint", func(t *testing.T) {
		s, convID, workspaceID := newStore(t)
		ctx := context.Background()
		approvalA := &approval.Approval{ID: util.GenerateID(), WorkspaceID: workspaceID, ConversationID: convID, Action: "refund_money"}
		approvalB := &approval.Approval{ID: util.GenerateID(), WorkspaceID: workspaceID, ConversationID: convID, Action: "refund_money"}
		if err := s.CreatePending(ctx, approvalA); err != nil {
			t.Fatalf("CreatePending A: %v", err)
		}
		if err := s.CreatePending(ctx, approvalB); err != nil {
			t.Fatalf("CreatePending B: %v", err)
		}
		if _, err := s.CheckpointForApproval(ctx, approvalA.ID, convID); err == nil {
			t.Fatal("expected error when no checkpoint")
		}
		if err := s.SaveCheckpoint(ctx, approvalA.ID, convID, []byte(`[{"role":"user","content":"approval-a"}]`)); err != nil {
			t.Fatalf("SaveCheckpoint A: %v", err)
		}
		if err := s.SaveCheckpoint(ctx, approvalB.ID, convID, []byte(`[{"role":"assistant","content":"approval-b"}]`)); err != nil {
			t.Fatalf("SaveCheckpoint B: %v", err)
		}
		got, err := s.CheckpointForApproval(ctx, approvalA.ID, convID)
		if err != nil {
			t.Fatalf("CheckpointForApproval A: %v", err)
		}
		if !jsonEq(string(got), `[{"role":"user","content":"approval-a"}]`) {
			t.Fatalf("approval A checkpoint mismatch: %s", got)
		}
		got, err = s.CheckpointForApproval(ctx, approvalB.ID, convID)
		if err != nil || !jsonEq(string(got), `[{"role":"assistant","content":"approval-b"}]`) {
			t.Fatalf("approval B checkpoint mismatch: %s err=%v", got, err)
		}
		if _, err := s.CheckpointForApproval(ctx, approvalA.ID, util.GenerateID()); err == nil {
			t.Fatal("checkpoint must reject a mismatched conversation")
		}
	})

	t.Run("ExecutionLeaseRetryAndFencing", func(t *testing.T) {
		s, convID, workspaceID := newStore(t)
		ctx := context.Background()
		a := &approval.Approval{
			ID: util.GenerateID(), WorkspaceID: workspaceID, ConversationID: convID, Action: "sensitive_action",
		}
		if err := s.CreatePending(ctx, a); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ResolvePending(ctx, a.ID, workspaceID, approval.StatusApproved, util.GenerateID()); err != nil {
			t.Fatal(err)
		}
		firstToken, err := s.BeginExecution(ctx, a.ID, convID)
		if err != nil || firstToken == "" {
			t.Fatalf("first claim token=%q err=%v", firstToken, err)
		}
		if err := s.FailExecution(ctx, a.ID, firstToken, "temporary provider error"); err != nil {
			t.Fatal(err)
		}
		ready, err := s.ListReadyResumes(ctx, 10)
		if err != nil || len(ready) != 1 || ready[0].ID != a.ID {
			t.Fatalf("failed execution should be retryable: %+v err=%v", ready, err)
		}
		secondToken, err := s.BeginExecution(ctx, a.ID, convID)
		if err != nil || secondToken == "" || secondToken == firstToken {
			t.Fatalf("second claim token=%q first=%q err=%v", secondToken, firstToken, err)
		}
		if err := s.CompleteExecution(ctx, a.ID, firstToken); err == nil {
			t.Fatal("stale execution token should not complete a reclaimed approval")
		}
		if err := s.RenewExecution(ctx, a.ID, secondToken); err != nil {
			t.Fatal(err)
		}
		if err := s.CompleteExecution(ctx, a.ID, secondToken); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, a.ID)
		if err != nil || got.ExecutionStatus != "completed" || got.ExecutionAttempts != 2 {
			t.Fatalf("completed approval=%+v err=%v", got, err)
		}
	})
}

func TestMemoryApprovalStore_Contract(t *testing.T) {
	runApprovalContract(t, func(t *testing.T) (approval.Store, string, string) {
		return approval.NewMemoryStore(), util.GenerateID(), "workspace-1"
	})
}
