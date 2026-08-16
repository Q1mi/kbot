package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr           string
	JWTSecret          string
	JWTIssuer          string
	LLMBaseURL         string
	LLMAPIKey          string
	LLMModel           string
	LLMTimeout         time.Duration
	SandboxRunnerURL   string
	SandboxRunnerToken string
	DatabaseURL        string
	WebhookSecret      string
	LarkEncryptKey     string
	ChannelWorkspaceID string
	ChannelAgentID     string
	ChannelAgentEnv    string
	BootstrapEmail     string
	BootstrapPassword  string
	BootstrapName      string
	ToolAllowedHosts   []string
}

func Load() Config {
	return Config{
		HTTPAddr:           valueOrDefault("KBOT_HTTP_ADDR", ":8080"),
		JWTSecret:          os.Getenv("KBOT_JWT_SECRET"),
		JWTIssuer:          valueOrDefault("KBOT_JWT_ISSUER", "kbot-course"),
		LLMBaseURL:         valueOrDefault("KBOT_LLM_BASE_URL", "http://127.0.0.1:8090/v1"),
		LLMAPIKey:          valueOrDefault("KBOT_LLM_API_KEY", "kbot-classroom-key"),
		LLMModel:           valueOrDefault("KBOT_LLM_MODEL", "kbot-classroom-mock"),
		LLMTimeout:         durationOrDefault("KBOT_LLM_TIMEOUT_SECONDS", 30*time.Second),
		SandboxRunnerURL:   valueOrDefault("KBOT_SANDBOX_RUNNER_URL", "http://127.0.0.1:8081"),
		SandboxRunnerToken: os.Getenv("KBOT_SANDBOX_RUNNER_TOKEN"),
		DatabaseURL:        valueOrDefault("KBOT_DATABASE_URL", "postgres://kbot:kbot@127.0.0.1:5432/kbot?sslmode=disable"),
		WebhookSecret:      os.Getenv("KBOT_WEBHOOK_SECRET"),
		LarkEncryptKey:     os.Getenv("KBOT_LARK_ENCRYPT_KEY"),
		ChannelWorkspaceID: os.Getenv("KBOT_CHANNEL_WORKSPACE_ID"),
		ChannelAgentID:     os.Getenv("KBOT_CHANNEL_AGENT_ID"),
		ChannelAgentEnv:    valueOrDefault("KBOT_CHANNEL_AGENT_ENV", "prod"),
		BootstrapEmail:     os.Getenv("KBOT_BOOTSTRAP_EMAIL"),
		BootstrapPassword:  os.Getenv("KBOT_BOOTSTRAP_PASSWORD"),
		BootstrapName:      valueOrDefault("KBOT_BOOTSTRAP_NAME", "Course Admin"),
		ToolAllowedHosts:   csvOrDefault("KBOT_TOOL_ALLOWED_HOSTS", []string{"crossborder-sim", "insurance-sim", "localhost", "127.0.0.1"}),
	}
}

func (c Config) Validate() error {
	if !strings.HasPrefix(c.HTTPAddr, ":") {
		return fmt.Errorf("KBOT_HTTP_ADDR must use :port format")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("KBOT_JWT_SECRET must contain at least 32 characters")
	}
	if strings.TrimSpace(c.JWTIssuer) == "" {
		return fmt.Errorf("KBOT_JWT_ISSUER is required")
	}
	if strings.TrimSpace(c.LLMBaseURL) == "" || strings.TrimSpace(c.LLMModel) == "" {
		return fmt.Errorf("LLM base URL and model are required")
	}
	if c.LLMTimeout <= 0 {
		return fmt.Errorf("LLM timeout must be positive")
	}
	if strings.TrimSpace(c.SandboxRunnerURL) == "" {
		return fmt.Errorf("KBOT_SANDBOX_RUNNER_URL is required")
	}
	if len(c.SandboxRunnerToken) < 32 {
		return fmt.Errorf("KBOT_SANDBOX_RUNNER_TOKEN must contain at least 32 characters")
	}
	if (c.WebhookSecret != "" || c.LarkEncryptKey != "") && (strings.TrimSpace(c.ChannelWorkspaceID) == "" || strings.TrimSpace(c.ChannelAgentID) == "") {
		return fmt.Errorf("enabled channels require KBOT_CHANNEL_WORKSPACE_ID and KBOT_CHANNEL_AGENT_ID")
	}
	if c.BootstrapEmail != "" && len(c.BootstrapPassword) < 8 {
		return fmt.Errorf("KBOT_BOOTSTRAP_PASSWORD must contain at least 8 characters when bootstrap is enabled")
	}
	return nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func csvOrDefault(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
