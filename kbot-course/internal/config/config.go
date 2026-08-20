package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr   string
	JWTSecret  string
	JWTIssuer  string
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string
	LLMTimeout time.Duration
}

func Load() Config {
	return Config{
		HTTPAddr:   valueOrDefault("KBOT_HTTP_ADDR", ":8080"),
		JWTSecret:  os.Getenv("KBOT_JWT_SECRET"),
		JWTIssuer:  valueOrDefault("KBOT_JWT_ISSUER", "kbot-course"),
		LLMBaseURL: valueOrDefault("KBOT_LLM_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"),
		LLMAPIKey:  firstNonEmptyEnv("KBOT_LLM_API_KEY", "ARK_API_KEY"),
		LLMModel:   valueOrDefault("KBOT_LLM_MODEL", "doubao-seed-2-0-lite-260215"),
		LLMTimeout: durationOrDefault("KBOT_LLM_TIMEOUT_SECONDS", 30*time.Second),
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
	if strings.TrimSpace(c.LLMAPIKey) == "" {
		return fmt.Errorf("KBOT_LLM_API_KEY or ARK_API_KEY is required")
	}
	if c.LLMTimeout <= 0 {
		return fmt.Errorf("LLM timeout must be positive")
	}
	return nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
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
