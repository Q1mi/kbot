package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/a2ui"
	"github.com/Q1mi/kbot/internal/api/middleware"
	markdown "github.com/Q1mi/kbot/internal/connector/markdown_folder"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	platformeval "github.com/Q1mi/kbot/internal/platform/eval"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	platformteam "github.com/Q1mi/kbot/internal/platform/team"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

type ControlPlane struct {
	Agents         *agent.Service
	Approvals      *approval.Service
	Audit          *audit.Ledger
	Tools          *platformtool.Registry
	ToolExecutor   *tooling.Executor
	KBs            *kb.Service
	Search         *retriever.KnowledgeSearch
	Prompts        *prompt.Service
	Profiles       *modelconfig.Registry
	Skills         *skill.Service
	Guard          *guard.Service
	Evaluator      *platformeval.Service
	EvalData       *platformeval.Catalog
	Teams          *platformteam.Service
	Webhook        http.Handler
	Lark           http.Handler
	ApprovalWorker interface{ Wake() }
	Readiness      func(context.Context) error
}

type runtimeEvalAgent struct {
	runtime     ChatRuntime
	agents      *agent.Service
	workspaceID string
	userID      string
	agentID     string
	versionID   string
}

func (a runtimeEvalAgent) Run(ctx context.Context, input string) (platformeval.Output, error) {
	conversation, err := a.agents.CreateConversationForVersion(ctx, a.workspaceID, a.agentID, a.versionID, "eval:"+a.userID)
	if err != nil {
		return platformeval.Output{}, err
	}
	output := platformeval.Output{}
	err = a.runtime.ChatStream(ctx, engine.ChatRequest{ConversationID: conversation.ID, WorkspaceID: a.workspaceID, UserID: "eval:" + a.userID, Message: input}, func(event engine.Event) error {
		if event.Type == "answer_done" {
			output.Content = event.Text
		}
		if event.Type == "tool_finished" {
			if data, ok := event.Data.(map[string]any); ok {
				if name, ok := data["name"].(string); ok {
					output.Tools = append(output.Tools, name)
				}
			}
		}
		return nil
	})
	return output, err
}

type agentConfigRequest struct {
	Name                  string   `json:"name"`
	Template              string   `json:"template"`
	SystemPrompt          string   `json:"system_prompt"`
	SystemPromptVersionID string   `json:"system_prompt_version_id"`
	ToolIDs               []string `json:"tool_ids"`
	SkillVersionIDs       []string `json:"skill_version_ids"`
	KBIDs                 []string `json:"kb_ids"`
	MaxSteps              int      `json:"max_steps"`
}

func (req agentConfigRequest) snapshot(workspaceID, agentID, versionID string) engine.AgentSnapshot {
	if req.MaxSteps <= 0 {
		req.MaxSteps = 4
	}
	if req.SystemPrompt == "" && req.SystemPromptVersionID == "" {
		req.SystemPrompt = "You are a helpful kbot course agent."
	}
	return engine.AgentSnapshot{
		ID: versionID, AgentID: agentID, WorkspaceID: workspaceID,
		SystemPrompt: req.SystemPrompt, MaxSteps: req.MaxSteps,
		PromptVersionID: req.SystemPromptVersionID,
		ToolVersionIDs:  append([]string(nil), req.ToolIDs...), SkillVersionIDs: append([]string(nil), req.SkillVersionIDs...),
		KnowledgeVersionIDs: append([]string(nil), req.KBIDs...),
	}
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
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if control.Readiness != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if err := control.Readiness(ctx); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	router.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("kbot_up 1\n"))
	})
	if control.Webhook != nil {
		router.Method(http.MethodPost, "/api/v1/integrations/webhook", control.Webhook)
	}
	if control.Lark != nil {
		router.Method(http.MethodPost, "/api/v1/integrations/lark/events", control.Lark)
	}
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
		protected.Get("/api/v1/users", func(w http.ResponseWriter, r *http.Request) {
			users, err := iamService.ListUsers(r.Context())
			if err != nil {
				http.Error(w, "list users", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, users)
		})
		protected.Post("/api/v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				ParentID    string `json:"parent_id"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			workspace, err := iamService.CreateWorkspace(r.Context(), middleware.UserID(r.Context()), req.Name, req.Description, req.ParentID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, workspace)
		})
		protected.With(middleware.Workspace(iamService)).Get("/api/v1/context", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{
				"user_id":      middleware.UserID(r.Context()),
				"workspace_id": middleware.WorkspaceID(r.Context()),
				"role":         middleware.WorkspaceRole(r.Context()),
			})
		})
		registerTeamRoutes(protected, iamService, runtime, control)
		if control.Agents != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Agents.ListAgents(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
				var req agentConfigRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				workspaceID := middleware.WorkspaceID(r.Context())
				item, err := control.Agents.CreateAgent(r.Context(), workspaceID, req.Name, req.Template)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				versionID := fmt.Sprintf("agent-version-%d", time.Now().UnixNano())
				version := domain.AgentVersion{ID: versionID, AgentID: item.ID, WorkspaceID: workspaceID, Version: 1, SystemPrompt: req.SystemPrompt, CreatedAt: time.Now().UTC()}
				if err := control.Agents.Publish(r.Context(), version, req.snapshot(workspaceID, item.ID, versionID)); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if err := control.Agents.Promote(r.Context(), workspaceID, item.ID, "dev", versionID); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, item)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/agents/{agentID}", func(w http.ResponseWriter, r *http.Request) {
				item, err := control.Agents.GetAgent(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "agentID"))
				if err != nil {
					http.Error(w, "agent not found", http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, item)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/agents/{agentID}/versions", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Agents.ListVersionViews(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "agentID")))
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/agents/{agentID}/input-schema", func(w http.ResponseWriter, r *http.Request) {
				if _, err := control.Agents.GetAgent(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "agentID")); err != nil {
					http.Error(w, "agent not found", http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/agents/{agentID}/versions", func(w http.ResponseWriter, r *http.Request) {
				var req agentConfigRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				workspaceID, agentID := middleware.WorkspaceID(r.Context()), chi.URLParam(r, "agentID")
				if _, err := control.Agents.GetAgent(r.Context(), workspaceID, agentID); err != nil {
					http.Error(w, "agent not found", http.StatusNotFound)
					return
				}
				versions := control.Agents.ListVersions(r.Context(), workspaceID, agentID)
				versionID := fmt.Sprintf("agent-version-%d", time.Now().UnixNano())
				version := domain.AgentVersion{ID: versionID, AgentID: agentID, WorkspaceID: workspaceID, Version: len(versions) + 1, SystemPrompt: req.SystemPrompt, CreatedAt: time.Now().UTC()}
				if err := control.Agents.Publish(r.Context(), version, req.snapshot(workspaceID, agentID, versionID)); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, version)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/agents/{agentID}/promote", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					VersionID string `json:"version_id"`
					Env       string `json:"env"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				if control.EvalData != nil && req.Env != "dev" {
					if err := control.EvalData.RequirePassedRun(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "agentID"), req.VersionID); err != nil {
						http.Error(w, err.Error(), http.StatusPreconditionFailed)
						return
					}
				}
				if err := control.Agents.Promote(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "agentID"), req.Env, req.VersionID); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Agents.ListConversations(r.Context(), middleware.WorkspaceID(r.Context()), r.URL.Query().Get("agent_id")))
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/conversations/{conversationID}", func(w http.ResponseWriter, r *http.Request) {
				conversation, messages, err := control.Agents.ConversationDetail(r.Context(), middleware.WorkspaceID(r.Context()), middleware.UserID(r.Context()), chi.URLParam(r, "conversationID"))
				if err != nil {
					http.Error(w, "conversation not found", http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"conversation": conversation, "messages": messages})
			})
			if runtime != nil {
				protected.With(middleware.Workspace(iamService)).Post("/api/v1/agents/{agentID}/chat", func(w http.ResponseWriter, r *http.Request) {
					var req engine.ChatRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						http.Error(w, "invalid JSON", http.StatusBadRequest)
						return
					}
					workspaceID := middleware.WorkspaceID(r.Context())
					conversation, err := control.Agents.ResolveConversation(r.Context(), workspaceID, middleware.UserID(r.Context()), chi.URLParam(r, "agentID"), req.AgentEnvironment, req.ConversationID)
					if err != nil {
						http.Error(w, err.Error(), http.StatusNotFound)
						return
					}
					req.ConversationID, req.WorkspaceID, req.UserID = conversation.ID, workspaceID, middleware.UserID(r.Context())
					if err := control.Agents.AppendMessage(r.Context(), workspaceID, conversation.ID, "user", req.Message); err != nil {
						http.Error(w, err.Error(), http.StatusNotFound)
						return
					}
					var content, status string
					err = runtime.ChatStream(r.Context(), req, func(event engine.Event) error {
						if event.Type == "answer_done" {
							content = event.Text
						}
						if event.Type == "run_finished" {
							if data, ok := event.Data.(map[string]string); ok {
								status = data["status"]
							}
						}
						return nil
					})
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadGateway)
						return
					}
					if content != "" {
						_ = control.Agents.AppendMessage(r.Context(), workspaceID, conversation.ID, "assistant", content)
					}
					writeJSON(w, http.StatusOK, map[string]string{"content": content, "conversation_id": conversation.ID, "status": status})
				})
			}
		}
		if control.Tools != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Tools.ListTools(middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name           string `json:"name"`
					SourceType     string `json:"source_type"`
					Description    string `json:"description"`
					SchemaJSON     string `json:"schema_json"`
					EndpointConfig string `json:"endpoint_config"`
					AuthConfig     string `json:"auth_config"`
					RetryPolicy    string `json:"retry_policy"`
					Sensitive      bool   `json:"sensitive"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				executableEndpoint, err := executableToolEndpoint(req.SourceType, req.EndpointConfig)
				if err != nil {
					http.Error(w, "invalid endpoint_config", http.StatusBadRequest)
					return
				}
				versionID := fmt.Sprintf("tool-version-%d", time.Now().UnixNano())
				version := platformtool.Version{
					ID: versionID, ToolID: versionID, Version: 1,
					WorkspaceID: middleware.WorkspaceID(r.Context()), Name: req.Name,
					SourceType: req.SourceType, Description: req.Description,
					InputSchema: []byte(req.SchemaJSON), Endpoint: executableEndpoint,
					EndpointConfig: req.EndpointConfig,
					AuthConfig:     req.AuthConfig, RetryPolicy: req.RetryPolicy,
					Sensitive: req.Sensitive, Published: true, CreatedAt: time.Now().UTC(),
				}
				if err := control.Tools.Register(r.Context(), version); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, version)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/tools/{toolID}/versions", func(w http.ResponseWriter, r *http.Request) {
				versions, err := control.Tools.ListVersions(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "toolID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, toolVersionViews(versions))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/tools/{toolID}/versions", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					SchemaJSON     string `json:"schema_json"`
					EndpointConfig string `json:"endpoint_config"`
					AuthConfig     string `json:"auth_config"`
					RetryPolicy    string `json:"retry_policy"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				versions, err := control.Tools.ListVersions(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "toolID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				base := versions[0]
				endpoint, err := executableToolEndpoint(base.SourceType, req.EndpointConfig)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				version := platformtool.Version{ID: fmt.Sprintf("tool-version-%d", time.Now().UnixNano()), ToolID: base.ToolID, Version: base.Version + 1, WorkspaceID: base.WorkspaceID, Name: base.Name, SourceType: base.SourceType, Description: base.Description, InputSchema: []byte(req.SchemaJSON), Endpoint: endpoint, EndpointConfig: req.EndpointConfig, AuthConfig: req.AuthConfig, RetryPolicy: req.RetryPolicy, Sensitive: base.Sensitive, CreatedAt: time.Now().UTC()}
				if err := control.Tools.Register(r.Context(), version); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, toolVersionViews([]platformtool.Version{version})[0])
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/tools/{toolID}/versions/{versionID}/publish", func(w http.ResponseWriter, r *http.Request) {
				if err := control.Tools.PublishVersion(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "toolID"), chi.URLParam(r, "versionID")); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/tools/{toolID}/publish", func(w http.ResponseWriter, r *http.Request) {
				versions, err := control.Tools.ListVersions(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "toolID"))
				if err != nil || control.Tools.PublishVersion(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "toolID"), versions[0].ID) != nil {
					http.Error(w, "tool publish failed", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/tools/{toolID}/test", func(w http.ResponseWriter, r *http.Request) {
				if control.ToolExecutor == nil {
					http.Error(w, "tool sandbox unavailable", http.StatusServiceUnavailable)
					return
				}
				var req struct {
					Input json.RawMessage `json:"input"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				versions, err := control.Tools.ListVersions(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "toolID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				versionID := ""
				for _, version := range versions {
					if version.Published {
						versionID = version.ID
						break
					}
				}
				if versionID == "" {
					http.Error(w, "publish a tool version before testing", http.StatusConflict)
					return
				}
				started := time.Now()
				result, callErr := control.ToolExecutor.Execute(r.Context(), tooling.Call{WorkspaceID: middleware.WorkspaceID(r.Context()), ToolVersionID: versionID, Arguments: req.Input, IdempotencyKey: fmt.Sprintf("sandbox-%d", time.Now().UnixNano())})
				status, errorText := "success", ""
				if callErr != nil {
					status, errorText = "failed", callErr.Error()
				}
				writeJSON(w, http.StatusOK, map[string]any{"id": fmt.Sprintf("tool-run-%d", time.Now().UnixNano()), "tool_id": chi.URLParam(r, "toolID"), "tool_version_id": versionID, "input": string(req.Input), "output": string(result.Body), "status": status, "latency_ms": time.Since(started).Milliseconds(), "error": errorText})
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
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/kbs/{kbID}/connectors", func(w http.ResponseWriter, r *http.Request) {
				items, err := control.KBs.Connectors(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "kbID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, items)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/kbs/{kbID}/jobs", func(w http.ResponseWriter, r *http.Request) {
				items, err := control.KBs.Jobs(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "kbID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, items)
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
				writeJSON(w, http.StatusOK, control.Prompts.ListPrompts(middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name                  string         `json:"name"`
					Category              string         `json:"category"`
					Template              string         `json:"template"`
					VariablesSchema       string         `json:"variables_schema"`
					ModelProfileVersionID string         `json:"model_profile_version_id"`
					GenerationConfig      map[string]any `json:"generation_config"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				item, version, err := control.Prompts.Create(r.Context(), middleware.WorkspaceID(r.Context()), req.Name, req.Category, req.Template, req.VariablesSchema, req.ModelProfileVersionID, req.GenerationConfig)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{"prompt": item, "version": version})
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/prompts/{promptID}/versions", func(w http.ResponseWriter, r *http.Request) {
				versions, err := control.Prompts.ListVersions(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "promptID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, versions)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts/{promptID}/versions", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Template              string         `json:"template"`
					VariablesSchema       string         `json:"variables_schema"`
					ModelProfileVersionID string         `json:"model_profile_version_id"`
					GenerationConfig      map[string]any `json:"generation_config"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				version, err := control.Prompts.CreateVersion(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "promptID"), req.Template, req.VariablesSchema, req.ModelProfileVersionID, req.GenerationConfig)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, version)
			})
			for _, action := range []string{"promote", "rollback"} {
				action := action
				protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts/{promptID}/"+action, func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Env       string `json:"env"`
						VersionID string `json:"version_id"`
					}
					if json.NewDecoder(r.Body).Decode(&req) != nil || control.Prompts.Promote(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "promptID"), req.Env, req.VersionID) != nil {
						http.Error(w, "invalid prompt promotion", http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				})
			}
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts/{promptID}/render", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Env  string         `json:"env"`
					Vars map[string]any `json:"vars"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				rendered, err := control.Prompts.RenderEnvironment(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "promptID"), req.Env, req.Vars)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"rendered": rendered})
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts/{promptID}/rollouts", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Env                string `json:"env"`
					CandidateVersionID string `json:"candidate_version_id"`
					TrafficPercent     int    `json:"traffic_percent"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || control.Prompts.StartRollout(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "promptID"), req.Env, req.CandidateVersionID, req.TrafficPercent) != nil {
					http.Error(w, "invalid rollout", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			protected.With(middleware.Workspace(iamService)).Put("/api/v1/prompts/{promptID}/rollouts/traffic", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Env            string `json:"env"`
					TrafficPercent int    `json:"traffic_percent"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || control.Prompts.UpdateRollout(chi.URLParam(r, "promptID"), req.Env, req.TrafficPercent) != nil {
					http.Error(w, "invalid rollout", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			for _, action := range []string{"complete", "rollback"} {
				action := action
				protected.With(middleware.Workspace(iamService)).Post("/api/v1/prompts/{promptID}/rollouts/"+action, func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Env string `json:"env"`
					}
					if json.NewDecoder(r.Body).Decode(&req) != nil {
						http.Error(w, "invalid JSON", http.StatusBadRequest)
						return
					}
					var err error
					if action == "complete" {
						err = control.Prompts.CompleteRollout(chi.URLParam(r, "promptID"), req.Env)
					} else {
						err = control.Prompts.RollbackRollout(chi.URLParam(r, "promptID"), req.Env)
					}
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				})
			}
		}
		if control.Profiles != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/model-accounts", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Profiles.ListAccounts(middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/model-accounts", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name    string `json:"name"`
					Kind    string `json:"kind"`
					BaseURL string `json:"base_url"`
					APIKey  string `json:"api_key"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				item, err := control.Profiles.CreateAccount(middleware.WorkspaceID(r.Context()), req.Name, req.Kind, req.BaseURL, req.APIKey)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, item)
			})
			protected.With(middleware.Workspace(iamService)).Put("/api/v1/model-accounts/{accountID}/api-key", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					APIKey string `json:"api_key"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || control.Profiles.RotateAPIKey(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "accountID"), req.APIKey) != nil {
					http.Error(w, "invalid API key rotation", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/model-deployments", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Profiles.ListDeployments(middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/model-deployments", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ProviderAccountID string `json:"provider_account_id"`
					Name              string `json:"name"`
					ModelName         string `json:"model_name"`
					Region            string `json:"region"`
					TimeoutMS         int    `json:"timeout_ms"`
					MaxRetries        int    `json:"max_retries"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				item, err := control.Profiles.CreateDeployment(middleware.WorkspaceID(r.Context()), req.ProviderAccountID, req.Name, req.ModelName, req.Region, req.TimeoutMS, req.MaxRetries)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, item)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/model-profiles", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Profiles.ListProfileDefinitions(middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/model-profiles", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name                  string   `json:"name"`
					Description           string   `json:"description"`
					ClassificationMax     string   `json:"classification_max"`
					PrimaryDeploymentID   string   `json:"primary_deployment_id"`
					FallbackDeploymentIDs []string `json:"fallback_deployment_ids"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				profile, version, err := control.Profiles.CreateProfile(r.Context(), middleware.WorkspaceID(r.Context()), req.Name, req.Description, req.PrimaryDeploymentID, req.FallbackDeploymentIDs, req.ClassificationMax)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{"profile": profile, "version": version})
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/model-profile-versions", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Profiles.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/model-profiles/{profileID}/versions", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ClassificationMax     string   `json:"classification_max"`
					PrimaryDeploymentID   string   `json:"primary_deployment_id"`
					FallbackDeploymentIDs []string `json:"fallback_deployment_ids"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				version, err := control.Profiles.CreateProfileVersion(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "profileID"), req.PrimaryDeploymentID, req.FallbackDeploymentIDs, req.ClassificationMax)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, version)
			})
		}
		if control.Skills != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/skills", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Skills.ListSkills(middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/skills", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Category string `json:"category"`
					SkillMD  string `json:"skill_md"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				item, version, err := control.Skills.Create(r.Context(), middleware.WorkspaceID(r.Context()), req.Category, []byte(req.SkillMD))
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{"skill": item, "version": version})
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/skills/{skillID}/versions", func(w http.ResponseWriter, r *http.Request) {
				versions, err := control.Skills.ListVersions(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "skillID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, versions)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/skills/{skillID}/versions", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					SkillMD string `json:"skill_md"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				version, err := control.Skills.CreateVersion(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "skillID"), []byte(req.SkillMD))
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, version)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/skills/{skillID}/publish", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					VersionID string `json:"version_id"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || control.Skills.PublishVersion(middleware.WorkspaceID(r.Context()), chi.URLParam(r, "skillID"), req.VersionID) != nil {
					http.Error(w, "invalid skill publish", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
		if control.Approvals != nil {
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/conversations/{conversationID}/a2ui/actions", func(w http.ResponseWriter, r *http.Request) {
				var action a2ui.ActionRequest
				if err := json.NewDecoder(r.Body).Decode(&action); err != nil || action.Version != a2ui.Version {
					http.Error(w, "invalid A2UI action", http.StatusBadRequest)
					return
				}
				if action.Action.Name != "approval.approve" && action.Action.Name != "approval.reject" {
					http.Error(w, "A2UI action is not allowed", http.StatusBadRequest)
					return
				}
				approvalID, _ := action.Action.Context["approval_id"].(string)
				contextConversationID, _ := action.Action.Context["conversation_id"].(string)
				contextSurfaceID, _ := action.Action.Context["surface_id"].(string)
				conversationID := chi.URLParam(r, "conversationID")
				workspaceID := middleware.WorkspaceID(r.Context())
				request, err := control.Approvals.Get(r.Context(), workspaceID, approvalID)
				expectedComponentID := "approval-reject"
				if action.Action.Name == "approval.approve" {
					expectedComponentID = "approval-approve"
				}
				if err != nil || request.RunID != conversationID || contextConversationID != conversationID ||
					action.Action.SurfaceID != "approval-"+approvalID || contextSurfaceID != action.Action.SurfaceID ||
					action.Action.SourceComponentID != expectedComponentID {
					http.Error(w, "approval action binding does not match", http.StatusForbidden)
					return
				}
				approved := action.Action.Name == "approval.approve"
				if err := control.Approvals.Decide(r.Context(), workspaceID, approvalID, middleware.UserID(r.Context()), approved); err != nil {
					http.Error(w, err.Error(), http.StatusConflict)
					return
				}
				if control.Audit != nil {
					_, _ = control.Audit.Append(r.Context(), audit.Event{
						WorkspaceID: workspaceID, ActorID: middleware.UserID(r.Context()),
						Action: "approval." + strings.TrimPrefix(action.Action.Name, "approval."), ResourceID: approvalID,
						Data: map[string]any{"conversation_id": conversationID, "tool_call_id": request.ToolCallID, "tool_version_id": request.ToolVersionID},
					})
				}
				status := "rejected"
				if approved {
					status = "approved"
					if control.ApprovalWorker != nil {
						control.ApprovalWorker.Wake()
					}
				}
				message := a2ui.Message{Version: a2ui.Version, UpdateDataModel: &a2ui.UpdateDataModel{SurfaceID: action.Action.SurfaceID, Path: "/status", Value: status}}
				if err := a2ui.Validate(message); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/a2ui+json")
				_ = json.NewEncoder(w).Encode(message)
			})
		}
		if control.Guard != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/guard/rules", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Guard.List(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/guard/rules", func(w http.ResponseWriter, r *http.Request) {
				var rule guard.RuleConfig
				if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				created, err := control.Guard.Create(r.Context(), middleware.WorkspaceID(r.Context()), rule)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, created)
			})
			protected.With(middleware.Workspace(iamService)).Put("/api/v1/guard/rules/{ruleID}", func(w http.ResponseWriter, r *http.Request) {
				var rule guard.RuleConfig
				if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				updated, err := control.Guard.Update(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "ruleID"), rule)
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, updated)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/guard/quotas", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Guard.ListQuotas(r.Context(), middleware.WorkspaceID(r.Context())))
			})
			protected.With(middleware.Workspace(iamService)).Put("/api/v1/guard/quotas/{metric}", func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Limit int64 `json:"limit"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				quota, err := control.Guard.SetQuota(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "metric"), input.Limit)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusOK, quota)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/guard/injection-logs", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, control.Guard.InjectionLogs(middleware.WorkspaceID(r.Context())))
			})
		}
		if control.Audit != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/audit/logs", func(w http.ResponseWriter, r *http.Request) {
				events, err := control.Audit.List(r.Context(), middleware.WorkspaceID(r.Context()))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				conversationID, actorID := r.URL.Query().Get("conversation_id"), r.URL.Query().Get("actor")
				filtered := events[:0]
				for _, event := range events {
					if conversationID != "" && event.ResourceID != conversationID {
						continue
					}
					if actorID != "" && event.ActorID != actorID {
						continue
					}
					filtered = append(filtered, event)
				}
				limit := 100
				if len(filtered) > limit {
					filtered = filtered[len(filtered)-limit:]
				}
				writeJSON(w, http.StatusOK, auditViews(filtered))
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/audit/exports", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ConversationID string `json:"conversation_id"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.ConversationID == "" {
					http.Error(w, "conversation_id is required", http.StatusBadRequest)
					return
				}
				events, err := control.Audit.List(r.Context(), middleware.WorkspaceID(r.Context()))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				var csv strings.Builder
				csv.WriteString("id,actor,action,resource_id,created_at,hash\n")
				count := 0
				for _, event := range events {
					if event.ResourceID != req.ConversationID {
						continue
					}
					fmt.Fprintf(&csv, "%q,%q,%q,%q,%q,%q\n", event.ID, event.ActorID, event.Action, event.ResourceID, event.CreatedAt.Format(time.RFC3339Nano), event.Hash)
					count++
				}
				key := "audit-" + req.ConversationID + ".csv"
				writeJSON(w, http.StatusOK, map[string]any{"key": key, "count": count, "url": "data:text/csv;base64," + base64.StdEncoding.EncodeToString([]byte(csv.String()))})
			})
		}
		if control.Evaluator != nil && control.EvalData != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/eval/datasets", func(w http.ResponseWriter, r *http.Request) {
				datasets, err := control.EvalData.ListDatasets(r.Context(), middleware.WorkspaceID(r.Context()))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, datasets)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/eval/datasets", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Name       string `json:"name"`
					TargetKind string `json:"target_kind"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				dataset, err := control.EvalData.CreateDataset(r.Context(), middleware.WorkspaceID(r.Context()), req.Name, req.TargetKind)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, dataset)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/eval/datasets/{datasetID}/cases", func(w http.ResponseWriter, r *http.Request) {
				cases, err := control.EvalData.Cases(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "datasetID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, cases)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/eval/datasets/{datasetID}/cases", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Input          string `json:"input"`
					Expected       string `json:"expected"`
					ConversationID string `json:"conversation_id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				metadata := ""
				if req.ConversationID != "" {
					metadata = fmt.Sprintf(`{"conversation_id":%q}`, req.ConversationID)
				}
				item, err := control.EvalData.AddCase(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "datasetID"), req.Input, req.Expected, metadata)
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusCreated, item)
			})
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/eval/datasets/{datasetID}/runs", func(w http.ResponseWriter, r *http.Request) {
				runs, err := control.EvalData.ListRuns(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "datasetID"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, runs)
			})
			if runtime != nil && control.Agents != nil {
				protected.With(middleware.Workspace(iamService)).Post("/api/v1/eval/runs", func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						DatasetID           string  `json:"dataset_id"`
						AgentID             string  `json:"agent_id"`
						AgentVersionID      string  `json:"agent_version_id"`
						JudgeTier           string  `json:"judge_tier"`
						JudgeAgentID        string  `json:"judge_agent_id"`
						JudgeAgentVersionID string  `json:"judge_agent_version_id"`
						Threshold           float64 `json:"threshold"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						http.Error(w, "invalid JSON", http.StatusBadRequest)
						return
					}
					if req.DatasetID == "" || req.AgentID == "" || req.AgentVersionID == "" {
						http.Error(w, "dataset and pinned agent version are required", http.StatusBadRequest)
						return
					}
					if err := platformeval.Gate(platformeval.Report{PassRate: 1}, req.Threshold); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					workspaceID := middleware.WorkspaceID(r.Context())
					stored, err := control.EvalData.Cases(r.Context(), workspaceID, req.DatasetID)
					if err != nil {
						http.Error(w, err.Error(), http.StatusNotFound)
						return
					}
					cases := make([]platformeval.Case, 0, len(stored))
					for _, item := range stored {
						cases = append(cases, platformeval.Case{ID: item.ID, Input: item.Input, ExpectedContains: item.Expected})
					}
					target := runtimeEvalAgent{runtime: runtime, agents: control.Agents, workspaceID: workspaceID, userID: middleware.UserID(r.Context()), agentID: req.AgentID, versionID: req.AgentVersionID}
					var judge platformeval.Judge = platformeval.ContainsJudge{}
					if req.JudgeTier != "" && req.JudgeTier != "deterministic" {
						if (req.JudgeTier != "light" && req.JudgeTier != "full") || req.JudgeAgentID == "" || req.JudgeAgentVersionID == "" {
							http.Error(w, "light/full judge requires a pinned judge agent version", http.StatusBadRequest)
							return
						}
						judgeAgent := runtimeEvalAgent{runtime: runtime, agents: control.Agents, workspaceID: workspaceID, userID: middleware.UserID(r.Context()), agentID: req.JudgeAgentID, versionID: req.JudgeAgentVersionID}
						judge = platformeval.LLMJudge{Tier: req.JudgeTier, Runner: func(ctx context.Context, prompt string) (string, error) {
							output, err := judgeAgent.Run(ctx, prompt)
							return output.Content, err
						}}
					}
					report, err := control.Evaluator.RunWithJudge(r.Context(), cases, target, judge)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					passed := platformeval.Gate(report, req.Threshold) == nil
					scores := make([]map[string]any, 0, len(report.Results))
					for _, result := range report.Results {
						scores = append(scores, map[string]any{"case_id": result.CaseID, "dimension": "correctness", "score": result.Score, "reason": strings.Join(result.Reasons, "; ")})
					}
					storedRun, err := control.EvalData.RecordRun(r.Context(), workspaceID, platformeval.StoredRun{
						DatasetID: req.DatasetID, AgentID: req.AgentID, AgentVersionID: req.AgentVersionID,
						JudgeKind: judge.Kind(), Threshold: req.Threshold, PassRate: report.PassRate, Passed: passed, Report: report,
					})
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					status := http.StatusOK
					if !passed {
						status = http.StatusUnprocessableEntity
					}
					writeJSON(w, status, map[string]any{"run_id": storedRun.ID, "agent_version_id": storedRun.AgentVersionID, "judge": storedRun.JudgeKind, "pass_rate": report.PassRate, "passed": passed, "total": len(report.Results), "scores": scores})
				})
			}
		}
		if control.Approvals != nil {
			protected.With(middleware.Workspace(iamService)).Get("/api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
				requests := control.Approvals.List(r.Context(), middleware.WorkspaceID(r.Context()))
				views := make([]map[string]any, 0)
				for _, request := range requests {
					if request.Status != approval.StatusPending {
						continue
					}
					views = append(views, map[string]any{"id": request.ID, "conversation_id": request.RunID, "action": request.ToolVersionID, "payload": string(request.Arguments), "status": request.Status, "created_at": request.CreatedAt})
				}
				writeJSON(w, http.StatusOK, views)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/approvals/{approvalID}/reject", func(w http.ResponseWriter, r *http.Request) {
				if err := control.Approvals.Decide(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "approvalID"), middleware.UserID(r.Context()), false); err != nil {
					http.Error(w, err.Error(), http.StatusConflict)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			protected.With(middleware.Workspace(iamService)).Post("/api/v1/approvals/{approvalID}/approve", func(w http.ResponseWriter, r *http.Request) {
				workspaceID, approvalID := middleware.WorkspaceID(r.Context()), chi.URLParam(r, "approvalID")
				if err := control.Approvals.Decide(r.Context(), workspaceID, approvalID, middleware.UserID(r.Context()), true); err != nil {
					http.Error(w, err.Error(), http.StatusConflict)
					return
				}
				if control.ApprovalWorker != nil {
					control.ApprovalWorker.Wake()
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
		protected.With(middleware.Workspace(iamService)).Get("/api/v1/observability", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"metrics_url": "/metrics", "healthz_url": "/healthz", "readyz_url": "/readyz",
				"otlp_endpoint": "", "traces_enabled": true, "workspace_id": middleware.WorkspaceID(r.Context()),
			})
		})
		if runtime != nil {
			stream := NewStreamHandler(runtime)
			if control.Agents != nil {
				stream.WithConversations(control.Agents)
			}
			protected.With(middleware.Workspace(iamService)).Post("/stream/agents/{agentID}/chat", stream.ServeHTTP)
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

func executableToolEndpoint(sourceType, raw string) (string, error) {
	var endpoint struct {
		URL      string `json:"url"`
		SDKName  string `json:"sdk_name"`
		CardURL  string `json:"card_url"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal([]byte(raw), &endpoint); err != nil {
		return "", err
	}
	switch sourceType {
	case "code_execution":
		language := strings.TrimSpace(endpoint.Language)
		if language != "python" && language != "bash" {
			return "", fmt.Errorf("code_execution language must be python or bash")
		}
		return language, nil
	case "internal_sdk":
		return endpoint.SDKName, nil
	case "a2a":
		return endpoint.CardURL, nil
	default:
		return endpoint.URL, nil
	}
}

func toolVersionViews(versions []platformtool.Version) []map[string]any {
	result := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		status := "draft"
		if version.Published {
			status = "published"
		}
		result = append(result, map[string]any{
			"id": version.ID, "tool_id": version.ToolID, "version": version.Version,
			"schema_json": string(version.InputSchema), "endpoint_config": version.EndpointConfig,
			"auth_config": version.AuthConfig, "retry_policy": version.RetryPolicy,
			"status": status, "created_at": version.CreatedAt,
		})
	}
	return result
}

func auditViews(events []audit.Event) []map[string]any {
	result := make([]map[string]any, 0, len(events))
	for _, event := range events {
		data, _ := json.Marshal(event.Data)
		result = append(result, map[string]any{
			"id": event.ID, "actor": event.ActorID, "action": event.Action,
			"resource_type": "conversation", "resource_id": event.ResourceID,
			"after_json": string(data), "created_at": event.CreatedAt,
		})
	}
	return result
}
