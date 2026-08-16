package iam_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/iam"
)

func TestCreateUserValidatesAndNormalizesCredentials(t *testing.T) {
	service := iam.NewService(platform.NewMemoryIAMStore(), []byte("test-jwt-key-at-least-32-characters"))
	if _, err := service.CreateUser(context.Background(), "invalid", "long-enough", "User"); err == nil {
		t.Fatal("invalid email was accepted")
	}
	if _, err := service.CreateUser(context.Background(), "user@example.com", "short", "User"); err == nil {
		t.Fatal("short password was accepted")
	}
	if _, err := service.CreateUser(context.Background(), "user@example.com", "long-enough", "  "); err == nil {
		t.Fatal("empty name was accepted")
	}
	user, err := service.CreateUser(context.Background(), " USER@Example.COM ", "long-enough", " User ")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "user@example.com" || user.Name != "User" {
		t.Fatalf("user was not normalized: %+v", user)
	}
}

func TestWorkspaceOwnerMutationsRequireOwnerAndProtectLastOwner(t *testing.T) {
	store := platform.NewMemoryIAMStore()
	service := iam.NewService(store, []byte("test-jwt-key-at-least-32-characters"))
	ctx := context.Background()
	owner, _ := service.CreateUser(ctx, "owner@example.com", "long-enough", "Owner")
	admin, _ := service.CreateUser(ctx, "workspace-admin@example.com", "long-enough", "Admin")
	member, _ := service.CreateUser(ctx, "member@example.com", "long-enough", "Member")
	workspace, err := service.CreateWorkspaceForUser(ctx, owner.ID, "Owner Rules", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, workspace.ID, admin.ID, iam.WorkspaceRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, admin.ID, workspace.ID, member.ID, iam.WorkspaceRoleOwner); !errors.Is(err, iam.ErrForbidden) {
		t.Fatalf("workspace admin promoted owner: %v", err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, admin.ID, workspace.ID, owner.ID, iam.WorkspaceRoleMember); !errors.Is(err, iam.ErrForbidden) {
		t.Fatalf("workspace admin changed owner: %v", err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, workspace.ID, owner.ID, iam.WorkspaceRoleAdmin); !errors.Is(err, iam.ErrLastOwner) {
		t.Fatalf("last owner was demoted: %v", err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, workspace.ID, member.ID, iam.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, workspace.ID, owner.ID, iam.WorkspaceRoleAdmin); err != nil {
		t.Fatalf("owner transfer failed: %v", err)
	}
}

func TestConcurrentOwnerDeletesKeepOneOwner(t *testing.T) {
	store := platform.NewMemoryIAMStore()
	service := iam.NewService(store, []byte("test-jwt-key-at-least-32-characters"))
	ctx := context.Background()
	globalAdmin, _ := service.CreateUser(ctx, "global@example.com", "long-enough", "Global")
	if err := store.SetUserRole(ctx, globalAdmin.ID, iam.GlobalRoleAdmin); err != nil {
		t.Fatal(err)
	}
	owner1, _ := service.CreateUser(ctx, "owner1@example.com", "long-enough", "Owner 1")
	owner2, _ := service.CreateUser(ctx, "owner2@example.com", "long-enough", "Owner 2")
	workspace, err := service.CreateWorkspaceForUser(ctx, owner1.ID, "Concurrent Owners", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner1.ID, workspace.ID, owner2.ID, iam.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, userID := range []string{owner1.ID, owner2.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			errs <- service.DeleteWorkspaceMember(ctx, globalAdmin.ID, workspace.ID, id)
		}(userID)
	}
	wg.Wait()
	close(errs)
	var deleted, protected int
	for err := range errs {
		switch {
		case err == nil:
			deleted++
		case errors.Is(err, iam.ErrLastOwner):
			protected++
		default:
			t.Fatalf("unexpected delete result: %v", err)
		}
	}
	if deleted != 1 || protected != 1 {
		t.Fatalf("expected one delete and one protected owner, got deleted=%d protected=%d", deleted, protected)
	}
}

func TestEnsureSeedWorkspacesIsIdempotent(t *testing.T) {
	store := platform.NewMemoryIAMStore()
	service := iam.NewService(store, []byte("test-jwt-key-at-least-32-characters"))
	ctx := context.Background()

	if _, err := service.CreateWorkspace(ctx, "学员自建空间", "应当保留", nil); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureSeedWorkspaces(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureSeedWorkspaces(ctx); err != nil {
		t.Fatal(err)
	}

	workspaces, err := service.ListWorkspaces(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 3 {
		t.Fatalf("expected one existing and two seed workspaces, got %d: %+v", len(workspaces), workspaces)
	}
	names := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		names[workspace.Name] = workspace.Description
	}
	for _, seed := range iam.DefaultSeedWorkspaces {
		if names[seed.Name] != seed.Description {
			t.Fatalf("seed workspace %q missing or description mismatch: %q", seed.Name, names[seed.Name])
		}
	}
}
