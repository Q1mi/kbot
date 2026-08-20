package middleware

import (
	"net/http"

	"github.com/Q1mi/kbot/internal/platform/iam"
)

func Auth(*iam.Service) Func {
	return func(next http.Handler) http.Handler {
		return next
	}
}
