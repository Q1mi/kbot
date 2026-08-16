package modelconfig_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/tool"
)

func TestProviderAccountRejectsPrivateEndpoint(t *testing.T) {
	cipher, err := modelconfig.NewCipher([]byte("test-encryption-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewMemoryStore(), cipher)
	svc.ConfigureEndpointPolicy(tool.NewEndpointPolicy(nil, false))
	_, err = svc.CreateProviderAccount(context.Background(), modelconfig.CreateProviderAccountRequest{
		WorkspaceID: "w1", Name: "metadata", Kind: "openai-compatible",
		BaseURL: "http://127.0.0.1:8080/v1", APIKey: "sk-test", CreatedBy: "u1",
	})
	if err == nil {
		t.Fatal("private provider endpoint should be rejected")
	}
}

func TestProviderProfileResolveKeepsAPIKeyPrivate(t *testing.T) {
	ctx := context.Background()
	cipher, err := modelconfig.NewCipher([]byte("test-encryption-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewMemoryStore(), cipher)
	account, err := svc.CreateProviderAccount(ctx, modelconfig.CreateProviderAccountRequest{
		WorkspaceID: "w1", Name: "project-a-prod", Kind: "openai-compatible",
		BaseURL: "https://example.com/v1", APIKey: "sk-secret", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !account.HasAPIKey {
		t.Fatal("expected masked api key indicator")
	}
	deployment, err := svc.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{
		WorkspaceID: "w1", ProviderAccountID: account.ID, Name: "primary",
		ModelName: "model-a", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.TimeoutMS != modelconfig.DefaultDeploymentTimeoutMS || deployment.MaxRetries != 0 {
		t.Fatalf("deployment defaults = timeout %d retries %d", deployment.TimeoutMS, deployment.MaxRetries)
	}
	priced, err := svc.UpdateDeploymentPricing(ctx, deployment.ID, modelconfig.UpdateDeploymentPricingRequest{
		WorkspaceID: "w1", InputPricePerMillion: 1.25,
		CachedInputPricePerMillion: 0.25, OutputPricePerMillion: 2.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if priced.InputPricePerMillion != 1.25 || priced.CachedInputPricePerMillion != 0.25 || priced.OutputPricePerMillion != 2.5 {
		t.Fatalf("unexpected deployment pricing: %+v", priced)
	}
	if _, err := svc.UpdateDeploymentPricing(ctx, deployment.ID, modelconfig.UpdateDeploymentPricingRequest{
		WorkspaceID: "another-workspace", InputPricePerMillion: 9,
	}); err == nil {
		t.Fatal("cross-workspace deployment pricing update should fail")
	}
	_, version, err := svc.CreateProfile(ctx, modelconfig.CreateProfileRequest{
		WorkspaceID: "w1", Name: "customer-service", PrimaryDeploymentID: deployment.ID,
		ClassificationMax: "confidential", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveProfile(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Deployments) != 1 || resolved.Deployments[0].APIKey != "sk-secret" {
		t.Fatalf("unexpected resolved profile: %+v", resolved)
	}
	if err := svc.RotateProviderAPIKey(ctx, "w1", account.ID, "sk-rotated"); err != nil {
		t.Fatal(err)
	}
	resolved, err = svc.ResolveProfile(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Deployments[0].APIKey != "sk-rotated" {
		t.Fatalf("api key rotation did not take effect: %+v", resolved)
	}
	if err := svc.RotateProviderAPIKey(ctx, "another-workspace", account.ID, "sk-denied"); err == nil {
		t.Fatal("cross-workspace api key rotation should fail")
	}
}

func TestProjectBudgetRequiresPricingOnFallbackDeployments(t *testing.T) {
	ctx := context.Background()
	cipher, err := modelconfig.NewCipher([]byte("test-encryption-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewMemoryStore(), cipher)
	account, err := svc.CreateProviderAccount(ctx, modelconfig.CreateProviderAccountRequest{
		WorkspaceID: "w1", Name: "provider", Kind: "openai-compatible",
		BaseURL: "https://example.com/v1", APIKey: "sk-test", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	createDeployment := func(name string) *modelconfig.ModelDeployment {
		deployment, createErr := svc.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{
			WorkspaceID: "w1", ProviderAccountID: account.ID, Name: name, ModelName: name, CreatedBy: "u1",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return deployment
	}
	primary := createDeployment("primary")
	fallback := createDeployment("fallback")
	if _, err := svc.UpdateDeploymentPricing(ctx, primary.ID, modelconfig.UpdateDeploymentPricingRequest{
		WorkspaceID: "w1", InputPricePerMillion: 1, OutputPricePerMillion: 2,
	}); err != nil {
		t.Fatal(err)
	}
	_, version, err := svc.CreateProfile(ctx, modelconfig.CreateProfileRequest{
		WorkspaceID: "w1", Name: "profile", PrimaryDeploymentID: primary.ID,
		FallbackDeploymentIDs: []string{fallback.ID}, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := &modelconfig.ProjectBinding{
		WorkspaceID: "w1", Env: "prod", ModelProfileVersionID: version.ID, MonthlyBudget: 10,
	}
	if err := svc.BindProject(ctx, binding); err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("unpriced fallback was accepted: %v", err)
	}
	if _, err := svc.UpdateDeploymentPricing(ctx, fallback.ID, modelconfig.UpdateDeploymentPricingRequest{
		WorkspaceID: "w1", InputPricePerMillion: 1, OutputPricePerMillion: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.BindProject(ctx, binding); err != nil {
		t.Fatalf("fully priced profile was rejected: %v", err)
	}
}

func TestCreateProfileRejectsDuplicateNameInWorkspace(t *testing.T) {
	ctx := context.Background()
	cipher, err := modelconfig.NewCipher([]byte("test-encryption-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewMemoryStore(), cipher)
	account, err := svc.CreateProviderAccount(ctx, modelconfig.CreateProviderAccountRequest{
		WorkspaceID: "w1", Name: "provider", Kind: "openai-compatible",
		BaseURL: "https://example.com/v1", APIKey: "sk-test", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := svc.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{
		WorkspaceID: "w1", ProviderAccountID: account.ID, Name: "primary",
		ModelName: "model-a", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	create := func(name string) error {
		_, _, createErr := svc.CreateProfile(ctx, modelconfig.CreateProfileRequest{
			WorkspaceID: "w1", Name: name, PrimaryDeploymentID: deployment.ID,
			ClassificationMax: "internal", CreatedBy: "u1",
		})
		return createErr
	}
	if err := create("商品运营 Profile"); err != nil {
		t.Fatal(err)
	}
	if err := create("  商品运营 Profile  "); !errors.Is(err, modelconfig.ErrProfileNameExists) {
		t.Fatalf("expected ErrProfileNameExists, got %v", err)
	}
	if err := create("商品运营 Profile"); !errors.Is(err, modelconfig.ErrProfileNameExists) {
		t.Fatalf("expected idempotent duplicate rejection, got %v", err)
	}
	profiles, err := svc.ListProfiles(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("duplicate profile was persisted: %+v", profiles)
	}
}

func TestEnsureSeedWorkspaceConfigIsIdempotent(t *testing.T) {
	ctx := context.Background()
	cipher, err := modelconfig.NewCipher([]byte("test-encryption-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewMemoryStore(), cipher)
	req := modelconfig.SeedWorkspaceConfig{
		WorkspaceID: "commerce", ProviderName: "电商专用模型账号",
		ProviderKind: "openai-compatible", BaseURL: "https://example.com/v1",
		APIKey: "sk-classroom", ModelName: "classroom-model", Region: "classroom",
		Profiles: []modelconfig.SeedProfile{
			{Name: "商品运营 Profile", DeploymentName: "商品运营模型", ClassificationMax: "internal"},
			{Name: "供应链协同 Profile", DeploymentName: "供应链协同模型", ClassificationMax: "confidential"},
		},
	}
	if err := svc.EnsureSeedWorkspaceConfig(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureSeedWorkspaceConfig(ctx, req); err != nil {
		t.Fatal(err)
	}
	accounts, _ := svc.ListProviderAccounts(ctx, req.WorkspaceID)
	deployments, _ := svc.ListDeployments(ctx, req.WorkspaceID)
	profiles, _ := svc.ListProfiles(ctx, req.WorkspaceID)
	if len(accounts) != 1 || len(deployments) != 2 || len(profiles) != 2 {
		t.Fatalf("seed resources were duplicated: accounts=%d deployments=%d profiles=%d", len(accounts), len(deployments), len(profiles))
	}
	for _, profile := range profiles {
		versions, listErr := svc.ListProfileVersions(ctx, profile.ID)
		if listErr != nil || len(versions) != 1 || versions[0].Version != 1 {
			t.Fatalf("profile %q should have exactly one v1: versions=%+v err=%v", profile.Name, versions, listErr)
		}
	}
}
