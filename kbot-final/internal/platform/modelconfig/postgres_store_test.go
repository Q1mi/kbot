//go:build integration

package modelconfig_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/runtime/llm"
)

func TestPostgresModelConfigRoundTrip(t *testing.T) {
	db := testpg.Start(t)
	ctx := context.Background()
	_, _ = db.Exec(ctx, `
		TRUNCATE project_model_usage_reservations, model_call_logs, prompt_rollout_events, prompt_experiments,
		  conversation_runtime_configs, prompt_version_configs, project_model_bindings,
		  model_profile_versions, model_profiles, model_deployments, providers CASCADE`)

	cipher, err := modelconfig.NewCipher([]byte("integration-model-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewPostgresStore(db), cipher)
	account, err := svc.CreateProviderAccount(ctx, modelconfig.CreateProviderAccountRequest{
		WorkspaceID: "w-model", Name: "prod", Kind: "openai-compatible",
		BaseURL: "https://example.com/v1", APIKey: "sk-integration", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := svc.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{
		WorkspaceID: "w-model", ProviderAccountID: account.ID, Name: "primary",
		ModelName: "test-model", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, version, err := svc.CreateProfile(ctx, modelconfig.CreateProfileRequest{
		WorkspaceID: "w-model", Name: "main", PrimaryDeploymentID: deployment.ID,
		ClassificationMax: "internal", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveProfile(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Deployments[0].APIKey != "sk-integration" {
		t.Fatal("api key encryption round-trip failed")
	}
	if err := svc.RotateProviderAPIKey(ctx, "w-model", account.ID, "sk-rotated"); err != nil {
		t.Fatal(err)
	}
	resolved, err = svc.ResolveProfile(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Deployments[0].APIKey != "sk-rotated" {
		t.Fatal("rotated api key was not resolved")
	}
	priced, err := svc.UpdateDeploymentPricing(ctx, deployment.ID, modelconfig.UpdateDeploymentPricingRequest{
		WorkspaceID: "w-model", InputPricePerMillion: 1,
		CachedInputPricePerMillion: .2, OutputPricePerMillion: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if priced.InputPricePerMillion != 1 || priced.CachedInputPricePerMillion != .2 || priced.OutputPricePerMillion != 3 {
		t.Fatalf("unexpected deployment pricing: %+v", priced)
	}
	if err := svc.BindProject(ctx, &modelconfig.ProjectBinding{
		WorkspaceID: "w-model", Env: "dev", ModelProfileVersionID: version.ID,
		MonthlyBudget: .001, RPMLimit: 1, TPMLimit: 100,
	}); err != nil {
		t.Fatal(err)
	}
	reservationID, err := svc.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w-model", Env: "dev", ModelProfileVersionID: version.ID,
		DeploymentID: deployment.ID, ReservedTokens: 20, ReservedCost: .0005,
	})
	if err != nil || reservationID == "" {
		t.Fatalf("reserve project usage: id=%q err=%v", reservationID, err)
	}
	_, err = svc.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w-model", Env: "dev", ModelProfileVersionID: version.ID,
		DeploymentID: deployment.ID, ReservedTokens: 20, ReservedCost: .0005,
	})
	if !errors.Is(err, llm.ErrProjectQuotaExceeded) {
		t.Fatalf("expected PostgreSQL quota rejection, got %v", err)
	}
	if err := svc.FinalizeProjectUsage(ctx, reservationID, 12, .0003, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.BindProject(ctx, &modelconfig.ProjectBinding{
		WorkspaceID: "w-model", Env: "expiry", ModelProfileVersionID: version.ID,
		MonthlyBudget: .0005, TPMLimit: 20,
	}); err != nil {
		t.Fatal(err)
	}
	expiredID, err := svc.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w-model", Env: "expiry", ModelProfileVersionID: version.ID,
		DeploymentID: deployment.ID, ReservedTokens: 20, ReservedCost: .0005,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE project_model_usage_reservations SET expires_at=now()-interval '1 minute' WHERE id=$1`, expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReserveProjectUsage(ctx, llm.ProjectQuotaRequest{
		WorkspaceID: "w-model", Env: "expiry", ModelProfileVersionID: version.ID,
		DeploymentID: deployment.ID, ReservedTokens: 20, ReservedCost: .0005,
	}); err != nil {
		t.Fatalf("expired PostgreSQL reservation should be reconciled: %v", err)
	}
}
