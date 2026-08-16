package config

import (
	"reflect"
	"testing"
)

func TestSplitList(t *testing.T) {
	want := []string{"https://admin.example.com", "http://localhost:5173"}
	got := splitList(" https://admin.example.com, ,http://localhost:5173 ")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitList() = %#v, want %#v", got, want)
	}
}

func TestLoadAutoseedCourseAssets(t *testing.T) {
	t.Setenv("KBOT_AUTOSEED_COURSE_ASSETS", "true")
	if cfg := Load(); !cfg.AutoseedCourseAssets {
		t.Fatal("KBOT_AUTOSEED_COURSE_ASSETS=true was not loaded")
	}
}

func TestLoadSandboxRunner(t *testing.T) {
	t.Setenv("KBOT_SANDBOX_RUNNER_URL", "http://sandbox-runner:8081")
	t.Setenv("KBOT_SANDBOX_RUNNER_TOKEN", "sandbox-test-token")
	cfg := Load()
	if cfg.SandboxRunnerURL != "http://sandbox-runner:8081" || cfg.SandboxRunnerToken != "sandbox-test-token" {
		t.Fatalf("sandbox runner config = %q / %q", cfg.SandboxRunnerURL, cfg.SandboxRunnerToken)
	}
}

func TestValidateRejectsCourseDefaultsInProduction(t *testing.T) {
	cfg := Config{
		DatabaseURL: "postgres://db", JWTSecretKey: "dev-secret-key-32-chars-minimum",
		CredentialEncryptionKey: "dev-credential-key-minimum-32-chars",
		SandboxRunnerURL:        "http://sandbox-runner:8081",
		SandboxRunnerToken:      "kbot-dev-sandbox-runner-token-2026-minimum-32-chars",
		Environment:             "prod", CORSAllowedOrigins: []string{"https://admin.example.com"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production default secret rejection")
	}
}

func TestValidateAcceptsHardenedProductionConfig(t *testing.T) {
	cfg := Config{
		DatabaseURL: "postgres://db", JWTSecretKey: "jwt-production-secret-0000000000000001",
		CredentialEncryptionKey: "credential-production-secret-000000001",
		SandboxRunnerURL:        "http://sandbox-runner:8081",
		SandboxRunnerToken:      "sandbox-production-token-000000000001",
		Environment:             "prod", CORSAllowedOrigins: []string{"https://admin.example.com"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
