package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type replyRunner interface {
	Chat(context.Context, engine.ChatStreamRequest) (string, error)
}

type replyStore interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
}

type idempotentTextSender interface {
	SendTextIdempotent(context.Context, string, string, string, string) error
}

// ReplyTaskHandler 运行持久化飞书任务，并缓存模型结果供发送重试复用。
func ReplyTaskHandler(runtime replyRunner, sender idempotentTextSender, store replyStore) asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload jobs.LarkReplyPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return err
		}
		if payload.EventID == "" || payload.AgentID == "" || payload.Channel == "" || strings.TrimSpace(payload.Text) == "" {
			return fmt.Errorf("lark_reply requires event_id, agent_id, channel and text")
		}
		if runtime == nil || sender == nil || store == nil {
			return fmt.Errorf("lark reply dependencies are not configured")
		}
		replyKey := "integration:lark:reply:" + payload.EventID
		replyBytes, found, err := store.Get(ctx, replyKey)
		if err != nil {
			return err
		}
		if !found {
			reply, err := runtime.Chat(ctx, engine.ChatStreamRequest{
				AgentID: payload.AgentID, Message: payload.Text, UserID: "lark:" + payload.UserID,
			})
			if err != nil {
				return err
			}
			replyBytes = []byte(reply)
			if err := store.Set(ctx, replyKey, replyBytes, 24*time.Hour); err != nil {
				return fmt.Errorf("persist lark agent reply: %w", err)
			}
		}
		return sender.SendTextIdempotent(
			ctx, "chat_id", payload.Channel, string(replyBytes), jobs.LarkReplyID(payload.EventID),
		)
	}
}
