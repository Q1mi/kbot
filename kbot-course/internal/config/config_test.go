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

func TestFeishuCredentialsRequirePairAndFixedMapping(t *testing.T) {
	t.Setenv("KBOT_LARK_APP_ID", "cli-example")
	t.Setenv("KBOT_LARK_APP_SECRET", "example-secret")
	t.Setenv("KBOT_LARK_TENANT_KEY", "tenant-example")
	loaded := Load()
	if loaded.LarkAppID != "cli-example" || loaded.LarkAppSecret != "example-secret" || loaded.LarkTenantKey != "tenant-example" {
		t.Fatal("Feishu environment configuration was not loaded")
	}
	valid := Config{HTTPAddr: ":8080", JWTSecret: "0123456789abcdef0123456789abcdef", JWTIssuer: "test",
		LLMBaseURL: "http://localhost", LLMAPIKey: "test", LLMModel: "test", LLMTimeout: 1,
		SandboxRunnerURL: "http://localhost", SandboxRunnerToken: "0123456789abcdef0123456789abcdef",
		LarkAppID: "cli-example", LarkAppSecret: "example-secret", ChannelWorkspaceID: "ws", ChannelAgentID: "agent", ChannelAgentEnv: "dev"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.LarkAppID = "" }, func(c *Config) { c.LarkAppSecret = "" },
		func(c *Config) { c.ChannelWorkspaceID = "" }, func(c *Config) { c.ChannelAgentID = "" },
	} {
		cfg := valid
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected incomplete Feishu configuration to fail")
		}
	}
}
