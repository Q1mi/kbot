package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/iam"
)

func NewRouter(iamService *iam.Service, runtimes ...ChatRuntime) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.Recoverer, middleware.RequestID)
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	router.Post("/api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Email, Password, Name string }
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		user, err := iamService.Register(r.Context(), req.Email, req.Password, req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, user)
	})
	router.Post("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Email, Password string }
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		result, err := iamService.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			http.Error(w, "invalid email or password", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	router.Group(func(protected chi.Router) {
		protected.Use(middleware.Auth(iamService))
		protected.Get("/api/v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
			workspaces, err := iamService.ListUserWorkspaces(r.Context(), middleware.UserID(r.Context()))
			if err != nil {
				http.Error(w, "list workspaces", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, workspaces)
		})
		protected.With(middleware.Workspace(iamService)).Get("/api/v1/context", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{
				"user_id":      middleware.UserID(r.Context()),
				"workspace_id": middleware.WorkspaceID(r.Context()),
				"role":         middleware.WorkspaceRole(r.Context()),
			})
		})
		if len(runtimes) > 0 && runtimes[0] != nil {
			protected.With(middleware.Workspace(iamService)).Post("/stream/agents/{agentID}/chat", NewStreamHandler(runtimes[0]).ServeHTTP)
		}
	})
	return router
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
