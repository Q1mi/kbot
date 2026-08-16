package lark

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	"github.com/Q1mi/kbot/internal/runtime/cache"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type jobRunner struct{ calls int }

func (r *jobRunner) Chat(_ context.Context, req engine.ChatStreamRequest) (string, error) {
	r.calls++
	return "reply to " + req.Message, nil
}

type jobSender struct {
	calls int
	key   string
}

func (s *jobSender) SendTextIdempotent(_ context.Context, kind, channel, text, key string) error {
	s.calls++
	s.key = key
	if kind != "chat_id" || channel != "chat-1" || text != "reply to hello" {
		return context.Canceled
	}
	return nil
}

func TestReplyTaskHandlerReusesPersistedModelReply(t *testing.T) {
	payload, _ := json.Marshal(jobs.LarkReplyPayload{
		EventID: "evt-1", AgentID: "agent-1", Channel: "chat-1", UserID: "user-1", Text: "hello",
	})
	task := asynq.NewTask(jobs.TypeLarkReply, payload)
	runner := &jobRunner{}
	sender := &jobSender{}
	store := cache.NewMemoryIdemStore()
	handler := ReplyTaskHandler(runner, sender, store)
	if err := handler(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := handler(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || sender.calls != 2 || sender.key != jobs.LarkReplyID("evt-1") {
		t.Fatalf("runner=%d sender=%d key=%q", runner.calls, sender.calls, sender.key)
	}
}
