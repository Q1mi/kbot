package platform

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
)

func TestMemoryAgentStore_ListAgents_ByWorkspaceSorted(t *testing.T) {
	s := NewMemoryAgentStore()
	ctx := context.Background()
	base := time.Now()
	mk := func(id, ws string, t time.Time) *domain.Agent {
		return &domain.Agent{ID: id, WorkspaceID: ws, Name: id, CreatedAt: t}
	}
	// 乱序插入,跨两个 workspace。
	_ = s.CreateAgent(ctx, mk("a2", "w1", base.Add(2*time.Second)))
	_ = s.CreateAgent(ctx, mk("a1", "w1", base.Add(1*time.Second)))
	_ = s.CreateAgent(ctx, mk("b1", "w2", base.Add(1*time.Second)))

	got, err := s.ListAgents(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 agents in w1, got %d", len(got))
	}
	// 升序(对齐 pg ORDER BY created_at)。
	if got[0].ID != "a1" || got[1].ID != "a2" {
		t.Fatalf("want [a1 a2], got [%s %s]", got[0].ID, got[1].ID)
	}
}

func TestMemoryIAMStore_ListUsersWorkspaces_PageAndSort(t *testing.T) {
	s := NewMemoryIAMStore()
	ctx := context.Background()
	base := time.Now()
	for i, id := range []string{"u-old", "u-mid", "u-new"} {
		_ = s.CreateUser(ctx, &domain.User{ID: id, Email: id + "@x.io", CreatedAt: base.Add(time.Duration(i) * time.Second)})
	}
	// 倒序(对齐 pg ORDER BY created_at DESC)。
	users, err := s.ListUsers(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 || users[0].ID != "u-new" || users[2].ID != "u-old" {
		t.Fatalf("unexpected user order: %+v", users)
	}
	// limit/offset 截取。
	page, _ := s.ListUsers(ctx, 1, 1)
	if len(page) != 1 || page[0].ID != "u-mid" {
		t.Fatalf("want [u-mid] for limit=1 offset=1, got %+v", page)
	}

	for i, id := range []string{"w-old", "w-new"} {
		_ = s.CreateWorkspace(ctx, &domain.Workspace{ID: id, Name: id, CreatedAt: base.Add(time.Duration(i) * time.Second)})
	}
	wss, _ := s.ListWorkspaces(ctx, 50, 0)
	if len(wss) != 2 || wss[0].ID != "w-new" {
		t.Fatalf("want w-new first, got %+v", wss)
	}
}
