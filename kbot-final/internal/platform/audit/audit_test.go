package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
)

type failingStore struct{}

func (failingStore) BatchInsert(context.Context, []*domain.AuditLog) error {
	return errors.New("storage unavailable")
}

func (failingStore) Query(context.Context, QueryFilter) ([]*domain.AuditLog, error) {
	return nil, nil
}

func TestWriteAndQuery(t *testing.T) {
	svc := NewService(NewMemoryStore())
	ctx := context.Background()

	svc.RecordConversation(ctx, "c1", "u1", "chat_turn", "你好")
	svc.RecordConversation(ctx, "c1", "u1", "chat_turn", "再见")
	svc.RecordConversation(ctx, "c2", "u2", "chat_turn", "other")
	svc.Record(ctx, "system", "guard_block:prompt_injection", "guard", "注入")

	svc.Close() // 刷净异步队列

	// 按 conversation_id 检索。
	logs, err := svc.Query(ctx, QueryFilter{ConversationID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs for c1, got %d", len(logs))
	}

	// 按 resource_type 检索（Guard 事件）。
	guardLogs, _ := svc.Query(ctx, QueryFilter{ResourceType: "guard"})
	if len(guardLogs) != 1 {
		t.Fatalf("expected 1 guard log, got %d", len(guardLogs))
	}
}

func TestWriterCountsPersistenceFailuresAndWritesAfterClose(t *testing.T) {
	writer := NewWriter(failingStore{})
	writer.Write(&domain.AuditLog{Action: "test"})
	writer.Close()
	if got := writer.Dropped(); got != 1 {
		t.Fatalf("Dropped()=%d, want 1", got)
	}

	writer.Write(&domain.AuditLog{Action: "late"})
	writer.Write(nil)
	if got := writer.Dropped(); got != 3 {
		t.Fatalf("Dropped()=%d, want 3", got)
	}
}
