package audit

// Audit Store 契约测试:memory 与 postgres 跑同一组用例(内部测试包,与既有 audit_test.go 一致)。

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
)

func sp(s string) *string { return &s }

func runAuditStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Run("BatchInsertAndQuery", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		base := time.Now().Add(-time.Hour)
		logs := []*domain.AuditLog{
			{Actor: "alice", Action: "create", ResourceType: "prompt", ResourceID: "p1", CreatedAt: base},
			{Actor: "bob", Action: "chat", ResourceType: "conversation", ResourceID: "c1",
				BeforeJSON: sp(`{"v":1}`), AfterJSON: sp(`{"v":2}`), IP: sp("10.0.0.1"), UserAgent: sp("curl/8"), CreatedAt: base.Add(time.Second)},
			{Actor: "alice", Action: "chat", ResourceType: "conversation", ResourceID: "c1", CreatedAt: base.Add(2 * time.Second)},
		}
		if err := s.BatchInsert(ctx, logs); err != nil {
			t.Fatalf("BatchInsert: %v", err)
		}

		// 全量:倒序最新优先。
		all, err := s.Query(ctx, QueryFilter{})
		if err != nil || len(all) != 3 {
			t.Fatalf("Query all: %v len=%d", err, len(all))
		}
		if all[0].Action != "chat" || all[0].Actor != "alice" || all[2].ResourceType != "prompt" {
			t.Fatalf("order mismatch: %+v", all)
		}

		// 按 actor 过滤。
		byActor, _ := s.Query(ctx, QueryFilter{Actor: "alice"})
		if len(byActor) != 2 {
			t.Fatalf("expected 2 alice logs, got %d", len(byActor))
		}

		// 按 resource_type 过滤。
		byType, _ := s.Query(ctx, QueryFilter{ResourceType: "conversation"})
		if len(byType) != 2 {
			t.Fatalf("expected 2 conversation logs, got %d", len(byType))
		}

		// 按 conversation 过滤 + 校验可空字段往返。
		byConv, _ := s.Query(ctx, QueryFilter{ConversationID: "c1"})
		if len(byConv) != 2 {
			t.Fatalf("expected 2 c1 logs, got %d", len(byConv))
		}
		var bob *domain.AuditLog
		for _, l := range byConv {
			if l.Actor == "bob" {
				bob = l
			}
		}
		if bob == nil || bob.BeforeJSON == nil || *bob.BeforeJSON != `{"v":1}` || bob.IP == nil || *bob.IP != "10.0.0.1" || bob.UserAgent == nil || *bob.UserAgent != "curl/8" {
			t.Fatalf("nullable fields round-trip mismatch: %+v", bob)
		}
		// alice 的那条 conversation 日志可空字段应为 nil。
		for _, l := range byConv {
			if l.Actor == "alice" && (l.BeforeJSON != nil || l.IP != nil) {
				t.Fatalf("expected nil nullable fields for alice log: %+v", l)
			}
		}

		// Limit。
		limited, _ := s.Query(ctx, QueryFilter{Limit: 1})
		if len(limited) != 1 || limited[0].Actor != "alice" {
			t.Fatalf("limit mismatch: %+v", limited)
		}
	})
}

func TestMemoryAuditStore_Contract(t *testing.T) {
	runAuditStoreContract(t, func(t *testing.T) Store {
		return NewMemoryStore()
	})
}
