package api

import (
	"net/http"

	"github.com/Q1mi/kbot/internal/platform/iam"
)

func NewRouter(*iam.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
