package agent_test

// Agent Store 契约测试(含会话/消息):memory 与 postgres 跑同一组用例。

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/util"
)

func runAgentStoreContract(t *testing.T, newStore func(t *testing.T) agent.Store) {
	ws := "ws-default"

	t.Run("AgentVersionsCurrentEnv", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		a := &domain.Agent{ID: util.GenerateID(), WorkspaceID: ws, Name: "助手", Template: "general", CreatedBy: "u1"}
		if err := s.CreateAgent(ctx, a); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
		got, err := s.GetAgent(ctx, a.ID)
		if err != nil || got.Name != "助手" || got.Template != "general" {
			t.Fatalf("GetAgent mismatch: %+v err=%v", got, err)
		}
		v1 := &domain.AgentVersion{ID: util.GenerateID(), AgentID: a.ID, Version: 1, SnapshotJSON: `{"prompt_version_id":"p1"}`, CreatedBy: "u1"}
		v2 := &domain.AgentVersion{ID: util.GenerateID(), AgentID: a.ID, Version: 2, SnapshotJSON: `{"prompt_version_id":"p2"}`, CreatedBy: "u1"}
		if err := s.CreateAgentVersion(ctx, v1); err != nil {
			t.Fatalf("CreateAgentVersion v1: %v", err)
		}
		if err := s.CreateAgentVersion(ctx, v2); err != nil {
			t.Fatalf("CreateAgentVersion v2: %v", err)
		}
		gv, err := s.GetAgentVersion(ctx, v1.ID)
		if err != nil || gv.SnapshotJSON != `{"prompt_version_id":"p1"}` {
			t.Fatalf("GetAgentVersion mismatch: %+v err=%v", gv, err)
		}
		// 新建版本自动成为 dev 当前版本 → 当前应是 v2。
		cur, err := s.GetAgentCurrentVersion(ctx, a.ID, "dev")
		if err != nil {
			t.Fatalf("GetAgentCurrentVersion(dev): %v", err)
		}
		if cur.Version != 2 || cur.SnapshotJSON != `{"prompt_version_id":"p2"}` {
			t.Fatalf("current dev version mismatch: %+v", cur)
		}
		versions, err := s.ListAgentVersions(ctx, a.ID)
		if err != nil || len(versions) != 2 || versions[0].Version != 2 {
			t.Fatalf("ListAgentVersions mismatch: %+v err=%v", versions, err)
		}
		if err := s.SetAgentEnvBinding(ctx, a.ID, "prod", v1.ID); err != nil {
			t.Fatalf("SetAgentEnvBinding(prod): %v", err)
		}
		prod, err := s.GetAgentCurrentVersion(ctx, a.ID, "prod")
		if err != nil || prod.ID != v1.ID {
			t.Fatalf("prod binding mismatch: %+v err=%v", prod, err)
		}
		// 未绑定的 env 报错。
		if _, err := s.GetAgentCurrentVersion(ctx, a.ID, "staging"); err == nil {
			t.Fatal("expected error for unbound env staging")
		}
	})

	t.Run("ConversationAndMessages", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		conv := &domain.Conversation{
			ID: util.GenerateID(), AgentID: util.GenerateID(), AgentVersionID: util.GenerateID(),
			WorkspaceID: ws, UserID: "user-1", Status: "active", Classification: "internal",
			StartedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.CreateConversation(ctx, conv); err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
		got, err := s.GetConversation(ctx, conv.ID)
		if err != nil {
			t.Fatalf("GetConversation: %v", err)
		}
		if got.UserID != "user-1" || got.Status != "active" || got.Classification != "internal" {
			t.Fatalf("conversation mismatch: %+v", got)
		}
		// 空会话:无消息。
		msgs, err := s.GetConversationMessages(ctx, conv.ID)
		if err != nil || len(msgs) != 0 {
			t.Fatalf("expected 0 messages, got %d err=%v", len(msgs), err)
		}
		// 用户消息。
		_ = s.CreateMessage(ctx, &domain.Message{ID: util.GenerateID(), ConversationID: conv.ID, Role: "user", Content: "你好"})
		// 助手带 tool_calls 的消息。
		tcid := "call-1"
		asst := &domain.Message{
			ID: util.GenerateID(), ConversationID: conv.ID, Role: "assistant", Content: "查一下",
			ToolCalls: []domain.ToolCall{{ID: "call-1", ToolID: "t1", Args: `{"q":"x"}`, LatencyMs: 5}},
		}
		if err := s.CreateMessage(ctx, asst); err != nil {
			t.Fatalf("CreateMessage(assistant): %v", err)
		}
		// 工具返回消息(带 tool_call_id)。
		if err := s.CreateMessage(ctx, &domain.Message{ID: util.GenerateID(), ConversationID: conv.ID, Role: "tool", Content: "结果", ToolCallID: &tcid}); err != nil {
			t.Fatalf("CreateMessage(tool): %v", err)
		}
		msgs, err = s.GetConversationMessages(ctx, conv.ID)
		if err != nil || len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d err=%v", len(msgs), err)
		}
		// 找到带 tool_calls 的助手消息,校验往返。
		var foundTC bool
		var foundToolMsg bool
		for _, m := range msgs {
			if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "call-1" && m.ToolCalls[0].Args == `{"q":"x"}` {
				foundTC = true
			}
			if m.Role == "tool" && m.ToolCallID != nil && *m.ToolCallID == "call-1" {
				foundToolMsg = true
			}
		}
		if !foundTC {
			t.Fatalf("assistant tool_calls round-trip failed: %+v", msgs)
		}
		if !foundToolMsg {
			t.Fatalf("tool message tool_call_id round-trip failed: %+v", msgs)
		}

		other := &domain.Conversation{
			ID: util.GenerateID(), AgentID: conv.AgentID, AgentVersionID: conv.AgentVersionID,
			WorkspaceID: ws, UserID: "user-2", Status: "active", Classification: "internal",
			StartedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.CreateConversation(ctx, other); err != nil {
			t.Fatal(err)
		}
		history, err := s.ListConversations(ctx, ws, "user-1", conv.AgentID, 10, 0)
		if err != nil || len(history) != 1 || history[0].ID != conv.ID {
			t.Fatalf("conversation history filter mismatch: %+v err=%v", history, err)
		}
	})

	t.Run("ConversationTurnLeaseAndAtomicCommit", func(t *testing.T) {
		s := newStore(t)
		turns, ok := s.(interface {
			ClaimConversationTurn(context.Context, string, bool) (string, error)
			RenewConversationTurn(context.Context, string, string) error
			CommitConversationTurn(context.Context, string, string, []*domain.Message, string) error
			ReleaseConversationTurn(context.Context, string, string, string) error
		})
		if !ok {
			t.Fatal("store does not implement conversation turn coordination")
		}
		ctx := context.Background()
		conv := &domain.Conversation{
			ID: util.GenerateID(), AgentID: util.GenerateID(), AgentVersionID: util.GenerateID(),
			WorkspaceID: ws, UserID: "turn-user", Status: "active", Classification: "internal",
			StartedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.CreateConversation(ctx, conv); err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
		token1, err := turns.ClaimConversationTurn(ctx, conv.ID, false)
		if err != nil {
			t.Fatalf("ClaimConversationTurn: %v", err)
		}
		if _, err := turns.ClaimConversationTurn(ctx, conv.ID, false); err == nil {
			t.Fatal("expected concurrent turn claim to fail")
		}
		if err := turns.RenewConversationTurn(ctx, conv.ID, token1); err != nil {
			t.Fatalf("RenewConversationTurn: %v", err)
		}
		messages := []*domain.Message{
			{ID: util.GenerateID(), ConversationID: conv.ID, Role: "user", Content: "question"},
			{ID: util.GenerateID(), ConversationID: conv.ID, Role: "assistant", Content: "answer"},
		}
		if err := turns.CommitConversationTurn(ctx, conv.ID, token1, messages, "active"); err != nil {
			t.Fatalf("CommitConversationTurn: %v", err)
		}
		token2, err := turns.ClaimConversationTurn(ctx, conv.ID, false)
		if err != nil {
			t.Fatalf("second ClaimConversationTurn: %v", err)
		}
		if err := turns.CommitConversationTurn(ctx, conv.ID, token1, nil, "active"); err == nil {
			t.Fatal("expected stale turn token to be rejected")
		}
		if err := turns.CommitConversationTurn(ctx, conv.ID, token2, nil, "awaiting_approval"); err != nil {
			t.Fatalf("commit awaiting approval: %v", err)
		}
		if _, err := turns.ClaimConversationTurn(ctx, conv.ID, false); err == nil {
			t.Fatal("expected normal chat claim to reject awaiting-approval conversation")
		}
		token3, err := turns.ClaimConversationTurn(ctx, conv.ID, true)
		if err != nil {
			t.Fatalf("resume ClaimConversationTurn: %v", err)
		}
		if err := turns.ReleaseConversationTurn(ctx, conv.ID, token3, "active"); err != nil {
			t.Fatalf("ReleaseConversationTurn: %v", err)
		}
		got, err := s.GetConversation(ctx, conv.ID)
		if err != nil || got.Status != "active" {
			t.Fatalf("conversation status mismatch: %+v err=%v", got, err)
		}
		stored, err := s.GetConversationMessages(ctx, conv.ID)
		if err != nil || len(stored) != 2 || stored[0].Content != "question" || stored[1].Content != "answer" {
			t.Fatalf("atomic messages mismatch: %+v err=%v", stored, err)
		}
	})
}

func TestMemoryAgentStore_Contract(t *testing.T) {
	runAgentStoreContract(t, func(t *testing.T) agent.Store {
		return platform.NewMemoryAgentStore()
	})
}
