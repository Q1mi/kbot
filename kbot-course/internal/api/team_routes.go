package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/iam"
	platformteam "github.com/Q1mi/kbot/internal/platform/team"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	runtimeteam "github.com/Q1mi/kbot/internal/runtime/team"
)

func registerTeamRoutes(router chi.Router, iamService *iam.Service, runtime ChatRuntime, control ControlPlane) {
	if control.Teams == nil {
		return
	}
	workspace := middleware.Workspace(iamService)
	router.With(workspace).Get("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		teams, err := control.Teams.List(r.Context(), middleware.WorkspaceID(r.Context()))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, teams)
	})
	router.With(workspace).Post("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Name    string                `json:"name"`
			Mode    string                `json:"mode"`
			Members []platformteam.Member `json:"members"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		item, version, err := control.Teams.Create(r.Context(), middleware.WorkspaceID(r.Context()), request.Name, request.Mode, request.Members)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"team": item, "version": version})
	})
	router.With(workspace).Get("/api/v1/teams/{teamID}/versions", func(w http.ResponseWriter, r *http.Request) {
		versions, err := control.Teams.ListVersions(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "teamID"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, versions)
	})
	router.With(workspace).Post("/api/v1/teams/{teamID}/versions", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Members  []platformteam.Member `json:"members"`
			AgentEnv string                `json:"agent_env"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		version, err := control.Teams.CreateVersion(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "teamID"), request.AgentEnv, request.Members)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, version)
	})
	router.With(workspace).Post("/api/v1/teams/{teamID}/promote", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			VersionID string `json:"version_id"`
			Env       string `json:"env"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := control.Teams.Promote(r.Context(), middleware.WorkspaceID(r.Context()), chi.URLParam(r, "teamID"), request.Env, request.VersionID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "promoted", "env": request.Env})
	})
	router.With(workspace).Post("/api/v1/teams/runs", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil || control.Agents == nil {
			http.Error(w, "team runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			TeamID string `json:"team_id"`
			Env    string `json:"env"`
			Input  string `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Input == "" {
			http.Error(w, "invalid team run", http.StatusBadRequest)
			return
		}
		workspaceID, userID := middleware.WorkspaceID(r.Context()), middleware.UserID(r.Context())
		spec, err := control.Teams.RunSpec(r.Context(), workspaceID, request.TeamID, request.Env)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		run := func(ctx context.Context, member runtimeteam.Member, input string) (string, error) {
			conversation, err := control.Agents.CreateConversationForVersion(ctx, workspaceID, member.AgentID, member.AgentVersionID, userID)
			if err != nil {
				return "", err
			}
			collector := teamMemberEventCollector{conversationID: conversation.ID}
			err = runtime.ChatStream(
				ctx,
				engine.ChatRequest{ConversationID: conversation.ID, WorkspaceID: workspaceID, UserID: userID, Message: input},
				collector.consume,
			)
			return collector.result(err)
		}
		members := make([]runtimeteam.Member, len(spec.Members))
		for index, member := range spec.Members {
			members[index] = runtimeteam.Member{AgentID: member.AgentID, AgentVersionID: member.AgentVersionID, Role: member.Role}
		}
		var final string
		var steps []runtimeteam.Step
		if runtimeteam.Mode(spec.Mode) == runtimeteam.ModePipeline {
			final, steps, err = runtimeteam.RunPipeline(r.Context(), members, request.Input, run)
		} else if len(members) >= 2 {
			supervisorRuntime, ok := runtime.(interface {
				RunSupervisorTeam(
					context.Context, runtimeteam.Member, []runtimeteam.Member, string, string, string, runtimeteam.MemberRunner,
				) (string, []runtimeteam.Step, error)
			})
			if !ok {
				http.Error(w, "Eino supervisor runtime unavailable", http.StatusServiceUnavailable)
				return
			}
			final, steps, err = supervisorRuntime.RunSupervisorTeam(
				r.Context(), members[0], members[1:], request.Input, workspaceID, userID, run,
			)
		} else {
			err = &teamRunError{"supervisor team needs a leader and worker"}
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"final": final, "steps": steps})
	})
}

type teamMemberEventCollector struct {
	conversationID string
	answer         string
	status         string
	approvalID     string
	toolName       string
	toolCallID     string
	toolVersionID  string
}

func (c *teamMemberEventCollector) consume(event engine.Event) error {
	switch event.Type {
	case "answer_done":
		c.answer = event.Text
	case "approval_requested":
		data, ok := event.Data.(map[string]string)
		if !ok {
			return &teamRunError{"invalid approval event from member runtime"}
		}
		c.approvalID = data["approval_id"]
		c.toolName = data["tool_name"]
		c.toolCallID = data["tool_call_id"]
		c.toolVersionID = data["tool_version_id"]
	case "run_finished":
		data, ok := event.Data.(map[string]string)
		if !ok {
			return &teamRunError{"invalid run status event from member runtime"}
		}
		c.status = data["status"]
	}
	return nil
}

func (c *teamMemberEventCollector) result(runErr error) (string, error) {
	if runErr != nil {
		return "", runErr
	}
	if c.status == "awaiting_approval" || c.approvalID != "" {
		return "", &engine.AwaitingApprovalError{
			ApprovalID: c.approvalID, ConversationID: c.conversationID, ToolName: c.toolName,
			ToolCallID: c.toolCallID, ToolVersionID: c.toolVersionID,
		}
	}
	return c.answer, nil
}

type teamRunError struct{ message string }

func (e *teamRunError) Error() string { return e.message }
