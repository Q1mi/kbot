package integration

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type dialogueRuntime struct {
	requests []engine.ChatRequest
	status   string
}

func (r *dialogueRuntime) ChatStream(_ context.Context, request engine.ChatRequest, emit engine.Emitter) error {
	r.requests = append(r.requests, request)
	if err := emit(engine.Event{Type: "answer_done", Text: "回答"}); err != nil {
		return err
	}
	if r.status == "awaiting_approval" {
		if err := emit(engine.Event{Type: "approval_requested", Data: map[string]string{"approval_id": "approval-1"}}); err != nil {
			return err
		}
	}
	return emit(engine.Event{Type: "run_finished", Data: map[string]string{"status": r.status}})
}

func TestDialoguePinsVersionAndSeparatesChannelIdentities(t *testing.T) {
	ctx := context.Background()
	agents := agent.NewService()
	item, err := agents.CreateAgent(ctx, "ws", "bot", "")
	if err != nil {
		t.Fatal(err)
	}
	promote := func(version string) {
		t.Helper()
		if err := agents.Publish(ctx, domain.AgentVersion{ID: version, AgentID: item.ID, WorkspaceID: "ws"}, engine.AgentSnapshot{ID: version, AgentID: item.ID, WorkspaceID: "ws"}); err != nil {
			t.Fatal(err)
		}
		if err := agents.Promote(ctx, "ws", item.ID, "dev", version); err != nil {
			t.Fatal(err)
		}
	}
	promote("v1")
	runtime := &dialogueRuntime{status: "completed"}
	dialogue, err := NewDialogue(agents, runtime, "ws", item.ID, "dev")
	if err != nil {
		t.Fatal(err)
	}
	message := Message{Source: "lark", AppID: "app1", TenantKey: "tenant1", ChatID: "chat1", UserID: "user1", Text: "你好", WorkspaceID: "forged-ws", ConversationID: "forged-conversation"}
	first, err := dialogue.Respond(ctx, message)
	if err != nil {
		t.Fatal(err)
	}
	promote("v2")
	second, err := dialogue.Respond(ctx, message)
	if err != nil {
		t.Fatal(err)
	}
	if second.ConversationID != first.ConversationID || second.AgentVersionID != "v1" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	seen := map[string]bool{first.ConversationID: true}
	for _, change := range []func(*Message){
		func(m *Message) { m.AppID = "app2" }, func(m *Message) { m.TenantKey = "tenant2" },
		func(m *Message) { m.ChatID = "chat2" }, func(m *Message) { m.UserID = "user2" },
	} {
		other := message
		change(&other)
		reply, err := dialogue.Respond(ctx, other)
		if err != nil {
			t.Fatal(err)
		}
		if seen[reply.ConversationID] || reply.AgentVersionID != "v2" {
			t.Fatalf("identity collision: %+v", reply)
		}
		seen[reply.ConversationID] = true
	}
	for _, request := range runtime.requests {
		if request.WorkspaceID != "ws" || request.UserID == message.UserID || request.ConversationID == message.ConversationID {
			t.Fatalf("unscoped request: %+v", request)
		}
	}
	message.Text = "/new"
	reset, err := dialogue.Respond(ctx, message)
	if err != nil || reset.ConversationID == first.ConversationID || reset.AgentVersionID != "v2" {
		t.Fatalf("reset=%+v err=%v", reset, err)
	}
	message.Text = "执行操作"
	for _, status := range []string{"blocked", "awaiting_approval"} {
		runtime.status = status
		reply, err := dialogue.Respond(ctx, message)
		if err != nil || reply.Status != status || reply.Text == "回答" || reply.Text == "" {
			t.Fatalf("reply=%+v err=%v", reply, err)
		}
		if status == "awaiting_approval" && reply.ApprovalID != "approval-1" {
			t.Fatalf("reply=%+v", reply)
		}
	}
}
