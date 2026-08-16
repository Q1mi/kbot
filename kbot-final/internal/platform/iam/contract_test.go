package iam_test

// IAM Store 契约测试:memory 与 postgres 两个实现跑同一组用例。
// 本文件(无 build tag)定义契约 + 始终运行的 memory 版;
// postgres_store_test.go(//go:build integration)用 dockertest 起真 PG 跑同一契约。
import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/util"
)

func runIAMStoreContract(t *testing.T, newStore func(t *testing.T) iam.Store) {
	t.Run("UserCreateAndGet", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		u := &domain.User{ID: util.GenerateID(), Email: "alice@example.com", Name: "Alice", PasswordHash: "hash1", Status: "active"}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		got, err := s.GetUserByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail: %v", err)
		}
		if got.Email != "alice@example.com" || got.Name != "Alice" || got.PasswordHash != "hash1" {
			t.Fatalf("user mismatch: %+v", got)
		}
		// 用 store 回传的 ID(PG 会规范化为 canonical UUID)做 GetByID。
		byID, err := s.GetUserByID(ctx, got.ID)
		if err != nil {
			t.Fatalf("GetUserByID(%s): %v", got.ID, err)
		}
		if byID.Email != got.Email {
			t.Fatalf("GetUserByID mismatch: %+v", byID)
		}
	})

	t.Run("GetUserByEmailMissing", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.GetUserByEmail(context.Background(), "nobody@example.com"); err == nil {
			t.Fatal("expected error for missing user, got nil")
		}
	})

	t.Run("WorkspaceCreateAndGet", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		ws := &domain.Workspace{ID: util.GenerateID(), Name: "Default", Description: "demo"}
		if err := s.CreateWorkspace(ctx, ws); err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		got, err := s.GetWorkspaceByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("GetWorkspaceByID: %v", err)
		}
		if got.Name != "Default" || got.Description != "demo" || got.ParentID != nil {
			t.Fatalf("workspace mismatch: %+v", got)
		}
	})
}

// memory 实现:始终运行(普通 `go test`)。
func TestMemoryIAMStore_Contract(t *testing.T) {
	runIAMStoreContract(t, func(t *testing.T) iam.Store {
		return platform.NewMemoryIAMStore()
	})
}
