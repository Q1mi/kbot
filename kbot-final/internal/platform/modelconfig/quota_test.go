package modelconfig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/llm"
)

func TestMemoryProjectQuotaReservesAndReconcilesUsage(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if err := store.UpsertProjectBinding(ctx, &ProjectBinding{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1",
		RPMLimit: 3, TPMLimit: 100, MonthlyBudget: 1,
	}); err != nil {
		t.Fatal(err)
	}
	first, err := store.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1", DeploymentID: "d1",
		ReservedTokens: 80, ReservedCost: .8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeProjectUsage(ctx, first, 10, .1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1", DeploymentID: "d1",
		ReservedTokens: 80, ReservedCost: .8,
	}); err != nil {
		t.Fatalf("actual usage reconciliation should release conservative reservation: %v", err)
	}
	_, err = store.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1", DeploymentID: "d1",
		ReservedTokens: 20, ReservedCost: .2,
	})
	if !errors.Is(err, llm.ErrProjectQuotaExceeded) {
		t.Fatalf("expected quota error, got %v", err)
	}
}

func TestMemoryProjectQuotaReconcilesExpiredReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.UpsertProjectBinding(ctx, &ProjectBinding{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1", TPMLimit: 100, MonthlyBudget: 1,
	})
	reservationID, err := store.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1", DeploymentID: "d1",
		ReservedTokens: 100, ReservedCost: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.usage[reservationID].expiresAt = time.Now().Add(-time.Minute)
	if _, err := store.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w1", Env: "dev", ModelProfileVersionID: "p1", DeploymentID: "d1",
		ReservedTokens: 100, ReservedCost: 1,
	}); err != nil {
		t.Fatalf("expired reservation should release tokens and budget: %v", err)
	}
	if store.usage[reservationID].status != "failed" {
		t.Fatalf("expired reservation status=%q", store.usage[reservationID].status)
	}
}

func TestMemoryProjectQuotaRejectsUnboundProfile(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.UpsertProjectBinding(ctx, &ProjectBinding{
		WorkspaceID: "w1", Env: "prod", ModelProfileVersionID: "approved-profile",
	})
	_, err := store.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w1", Env: "prod", ModelProfileVersionID: "other-profile", DeploymentID: "d1",
	})
	if !errors.Is(err, llm.ErrProjectQuotaExceeded) {
		t.Fatalf("expected unbound profile to be rejected, got %v", err)
	}
}
