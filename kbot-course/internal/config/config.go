package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr  string
	JWTSecret string
	JWTIssuer string
}

func Load() Config {
	return Config{
		HTTPAddr:  valueOrDefault("KBOT_HTTP_ADDR", ":8080"),
		JWTSecret: os.Getenv("KBOT_JWT_SECRET"),
		JWTIssuer: valueOrDefault("KBOT_JWT_ISSUER", "kbot-course"),
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
	return nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
