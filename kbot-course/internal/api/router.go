package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/api/middleware"
	markdown "github.com/Q1mi/kbot/internal/connector/markdown_folder"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
)

type ControlPlane struct {
	Tools    *platformtool.Registry
	KBs      *kb.Service
	Search   *retriever.KnowledgeSearch
	Prompts  *prompt.Service
	Profiles *modelconfig.Registry
	Skills   *skill.Service
}

func NewRouter(iamService *iam.Service, runtimes ...ChatRuntime) http.Handler {
	return NewRouterWithControlPlane(iamService, firstRuntime(runtimes), ControlPlane{})
}

func NewRouterWithControlPlane(iamService *iam.Service, runtime ChatRuntime, control ControlPlane) http.Handler {
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
		if control.Tools != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Tools.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name           string `json:"name"`
					SourceType     string `json:"source_type"`
					Description    string `json:"description"`
					SchemaJSON     string `json:"schema_json"`
					EndpointConfig string `json:"endpoint_config"`
					AuthConfig     string `json:"auth_config"`
					Sensitive      bool   `json:"sensitive"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				var endpoint struct {
					URL     string `json:"url"`
					SDKName string `json:"sdk_name"`
				}
				if err := json.Unmarshal([]byte(req.EndpointConfig), &endpoint); err != nil {
					http.Error(w, "invalid endpoint_config", http.StatusBadRequest)
					return
				}
				versionID := fmt.Sprintf("tool-version-%d", time.Now().UnixNano())
				executableEndpoint := endpoint.URL
				if req.SourceType == "internal_sdk" {
					executableEndpoint = endpoint.SDKName
				}
				version := platformtool.Version{
					ID: versionID, ToolID: versionID,
					WorkspaceID: middleware.WorkspaceID(r.Context()), Name: req.Name,
					SourceType: req.SourceType, Description: req.Description,
					InputSchema: []byte(req.SchemaJSON), Endpoint: executableEndpoint,
					AuthConfig: req.AuthConfig, HasAuth: req.AuthConfig != "",
					Sensitive: req.Sensitive, Published: true, CreatedAt: time.Now().UTC(),
				}
				if err := control.Tools.Register(r.Context(), version); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, version)
			})
		}
		if control.KBs != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/kbs", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.KBs.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/kbs", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name string `json:"name"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				base, err := control.KBs.Create(r.Context(), middleware.WorkspaceID(r.Context()), req.Name)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, base)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/kbs/{kbID}/connectors/markdown/sync", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					RootPath string `json:"root_path"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				job, err := control.KBs.Sync(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "kbID"), markdown.New(req.RootPath))
				if err != nil {
					writeJSON(w, http.StatusUnprocessableEntity, job)
					return
				}
				writeJSON(w, http.StatusAccepted, job)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/kbs/{kbID}/documents", func(w http.ResponseWriter, r *http.Request) {
				documents, err := control.KBs.Documents(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "kbID"))
				if err != nil {
					http.Error(w, "knowledge base not found", http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, documents)
			})
			if control.Search != nil {
				protected.With(middleware.Workspace(iamService)).Post("/api/v1/kbs/{kbID}/search", func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Query string `json:"query"`
						Mode  string `json:"mode"`
						TopK  int    `json:"top_k"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						http.Error(w, "invalid JSON", http.StatusBadRequest)
						return
					}
					if req.TopK == 0 {
						req.TopK = 5
					}
					results, err := control.Search.Search(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "kbID"), req.Query, req.Mode, req.TopK)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					writeJSON(w, http.StatusOK, results)
				})
			}
		}
		if control.Prompts != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/prompts", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Prompts.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name     string `json:"name"`
					Category string `json:"category"`
					Template string `json:"template"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				version := prompt.Version{ID: fmt.Sprintf("prompt-version-%d", time.Now().UnixNano()), WorkspaceID: middleware.WorkspaceID(r.Context()), Name: req.Name, Category: req.Category, Template: req.Template}
				if err := control.Prompts.Publish(r.Context(), version); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{"prompt": version, "version": version})
			})
		}
		if control.Profiles != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/model-profiles", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Profiles.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/model-profiles", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name              string `json:"name"`
					ClassificationMax string `json:"classification_max"`
					Provider          string `json:"provider"`
					Model             string `json:"model"`
					BaseURL           string `json:"base_url"`
					APIKey            string `json:"api_key"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				if req.ClassificationMax == "" {
					req.ClassificationMax = "internal"
				}
				if req.Provider == "" {
					req.Provider = "openai-compatible"
				}
				if req.Model == "" {
					req.Model = "kbot-course-model"
				}
				if req.BaseURL == "" {
					req.BaseURL = "http://mockllm:8081/v1"
				}
				profile := modelconfig.ProfileVersion{ID: fmt.Sprintf("model-profile-version-%d", time.Now().UnixNano()), WorkspaceID: middleware.WorkspaceID(r.Context()), Name: req.Name, ClassificationMax: req.ClassificationMax, Deployments: []modelconfig.Deployment{{Provider: req.Provider, Model: req.Model, BaseURL: req.BaseURL, APIKey: req.APIKey, HasAPIKey: req.APIKey != ""}}}
				if err := control.Profiles.Publish(r.Context(), profile); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{"profile": profile, "version": profile})
			})
		}
		if control.Skills != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/skills", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Skills.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/skills", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					SkillMD string `json:"skill_md"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				version, err := control.Skills.Publish(r.Context(), fmt.Sprintf("skill-version-%d", time.Now().UnixNano()), middleware.WorkspaceID(r.Context()), []byte(req.SkillMD))
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{"skill": version, "version": version})
			})
		}
		if runtime != nil {
			protected.With(middleware.Workspace(iamService)).Post("/stream/agents/{agentID}/chat", NewStreamHandler(runtime).ServeHTTP)
		}
	})
	return router
}

func firstRuntime(runtimes []ChatRuntime) ChatRuntime {
	if len(runtimes) == 0 {
		return nil
	}
	return runtimes[0]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
