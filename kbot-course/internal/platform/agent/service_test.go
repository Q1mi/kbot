package agent

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

func TestConversationPinsPromotedAgentVersion(t *testing.T) {
	service := NewService()
	publish := func(id, prompt string) {
		err := service.Publish(context.Background(), domain.AgentVersion{ID: id, AgentID: "support", WorkspaceID: "ws"}, engine.AgentSnapshot{ID: id, AgentID: "support", WorkspaceID: "ws", SystemPrompt: prompt, PromptVersionID: "prompt-" + id, ToolVersionIDs: []string{"refund-v1"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	publish("agent-v1", "version one")
	publish("agent-v2", "version two")
	if err := service.Promote(context.Background(), "ws", "support", "prod", "agent-v1"); err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateConversation(context.Background(), "ws", "support", "prod", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Promote(context.Background(), "ws", "support", "prod", "agent-v2"); err != nil {
		t.Fatal(err)
	}
	second, _ := service.CreateConversation(context.Background(), "ws", "support", "prod", "user")
	if first.AgentVersionID != "agent-v1" || second.AgentVersionID != "agent-v2" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	snapshot, _ := service.Snapshot(context.Background(), "ws", first.AgentVersionID)
	if snapshot.SystemPrompt != "version one" || snapshot.PromptVersionID != "prompt-agent-v1" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestSnapshotReturnsDefensiveCopy(t *testing.T) {
	service := NewService()
	version := domain.AgentVersion{ID: "v1", AgentID: "a", WorkspaceID: "ws"}
	_ = service.Publish(context.Background(), version, engine.AgentSnapshot{ID: "v1", AgentID: "a", WorkspaceID: "ws", ToolVersionIDs: []string{"tool-v1"}})
	snapshot, _ := service.Snapshot(context.Background(), "ws", "v1")
	snapshot.ToolVersionIDs[0] = "mutated"
	again, _ := service.Snapshot(context.Background(), "ws", "v1")
	if again.ToolVersionIDs[0] != "tool-v1" {
		t.Fatal("snapshot leaked mutable slice")
	}
}

func TestConversationMessagesAreOrderedAndWorkspaceScoped(t *testing.T) {
	service := NewService()
	version := domain.AgentVersion{ID: "v1", AgentID: "a", WorkspaceID: "ws"}
	if err := service.Publish(t.Context(), version, engine.AgentSnapshot{ID: "v1", AgentID: "a", WorkspaceID: "ws"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Promote(t.Context(), "ws", "a", "dev", "v1"); err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(t.Context(), "ws", "a", "dev", "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []struct{ role, content string }{{"user", "one"}, {"assistant", "two"}} {
		if err := service.AppendMessage(t.Context(), "ws", conversation.ID, message.role, message.content); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := service.ListMessages(t.Context(), "ws", conversation.ID)
	if err != nil || len(messages) != 2 || messages[0].Content != "one" || messages[1].Content != "two" {
		t.Fatalf("messages = %#v, %v", messages, err)
	}
	if _, err := service.ListMessages(t.Context(), "other", conversation.ID); err == nil {
		t.Fatal("expected cross-workspace history lookup to fail")
	}
}
