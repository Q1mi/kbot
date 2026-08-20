package config

import "testing"

func TestLoadAndValidate(t *testing.T) {
	t.Setenv("KBOT_HTTP_ADDR", ":9090")
	t.Setenv("KBOT_JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("KBOT_JWT_ISSUER", "kbot-test")
	t.Setenv("ARK_API_KEY", "test-ark-api-key")
	t.Setenv("KBOT_SANDBOX_RUNNER_TOKEN", "0123456789abcdef0123456789abcdef")

	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.SandboxRunnerURL != "http://127.0.0.1:8081" {
		t.Fatalf("SandboxRunnerURL = %q", cfg.SandboxRunnerURL)
	}
}

func TestValidateRejectsShortJWTSecret(t *testing.T) {
	err := (Config{HTTPAddr: ":8080", JWTSecret: "short", JWTIssuer: "kbot"}).Validate()
	if err == nil {
		t.Fatal("expected short JWT secret to fail")
	}
}
