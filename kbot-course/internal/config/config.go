package config

import "errors"

var ErrValidationNotImplemented = errors.New("config validation is not implemented")

type Config struct {
	HTTPAddr  string
	JWTSecret string
	JWTIssuer string
}

func Load() Config {
	return Config{HTTPAddr: ":8080", JWTIssuer: "kbot-course"}
}

func (Config) Validate() error {
	return ErrValidationNotImplemented
}
