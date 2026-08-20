package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestRegisterLoginAndParseToken(t *testing.T) {
	t.Parallel()

	svc := New(NewMemoryStore(), testSecret, "kbot-test")
	user, err := svc.Register(context.Background(), "student@example.com", "password123", "Student")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := svc.Login(context.Background(), user.Email, "password123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	userID, err := svc.ParseToken(result.Token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("user ID = %q, want %q", userID, user.ID)
	}
}

func TestLoginReturnsGenericError(t *testing.T) {
	t.Parallel()

	_, err := New(NewMemoryStore(), testSecret, "kbot-test").Login(
		context.Background(), "missing@example.com", "wrong-password",
	)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

func TestWorkspaceRoleComesFromMembership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	service := New(store, testSecret, "kbot-test")
	owner, err := service.Register(ctx, "owner@example.com", "password123", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := service.Register(ctx, "viewer@example.com", "password123", "Viewer")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := service.ListUserWorkspaces(ctx, owner.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("owner workspaces = %v, err = %v", workspaces, err)
	}
	if err := store.AddMembership(ctx, &domain.Membership{
		UserID: viewer.ID, WorkspaceID: workspaces[0].ID, Role: WorkspaceRoleViewer, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	role, err := service.WorkspaceRole(ctx, viewer.ID, workspaces[0].ID)
	if err != nil || role != WorkspaceRoleViewer {
		t.Fatalf("WorkspaceRole() = %q, %v", role, err)
	}
}
