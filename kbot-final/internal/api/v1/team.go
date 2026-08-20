package v1

import (
	"context"
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/domain"
	pteam "github.com/Q1mi/kbot/internal/platform/team"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/team"
	"github.com/go-chi/chi/v5"
)

// TeamHandler 处理持久化 Team 与 ad-hoc 编排请求。
type TeamHandler struct {
	svc     *pteam.Service
	runtime *engine.Engine
}

// NewTeamHandler 创建 Team 处理器。
func NewTeamHandler(svc *pteam.Service, runtime *engine.Engine) *TeamHandler {
	return &TeamHandler{svc: svc, runtime: runtime}
}

// Create 创建团队(建 v1 + 绑 dev)。
// @Summary  创建团队(建 v1 + 绑 dev)
// @Tags     teams
// @Security BearerAuth
// @Param    body  body      pteam.CreateTeamRequest  true  "团队 + 成员"
// @Success  201   {object}  map[string]interface{}
// @Router   /teams [post]
func (h *TeamHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req pteam.CreateTeamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(r.Context())
	t, v, err := h.svc.CreateTeam(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"team": t, "version": v})
}

// List 列出当前工作空间的团队。
// @Summary  列出团队
// @Tags     teams
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /teams [get]
func (h *TeamHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	teams, err := h.svc.ListTeams(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, teams)
}

// ListVersions 返回团队的不可变成员快照与环境绑定。
// @Summary  列出 Team 版本
// @Tags     teams
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /teams/{team_id}/versions [get]
func (h *TeamHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.svc.ListTeamVersions(
		r.Context(), chi.URLParam(r, "team_id"), middleware.GetWorkspaceIDFromContext(r.Context()),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// CreateVersion 创建团队成员快照并绑定 dev。
// @Summary  创建 Team 版本
// @Tags     teams
// @Security BearerAuth
// @Success  201  {object}  map[string]interface{}
// @Router   /teams/{team_id}/versions [post]
func (h *TeamHandler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	var req pteam.CreateTeamVersionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	version, err := h.svc.CreateTeamVersion(
		r.Context(), chi.URLParam(r, "team_id"), middleware.GetWorkspaceIDFromContext(r.Context()), req,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

// PromoteVersion 将团队版本绑定到目标环境。
// @Summary  提升 Team 版本
// @Tags     teams
// @Security BearerAuth
// @Success  200  {object}  map[string]string
// @Router   /teams/{team_id}/promote [post]
func (h *TeamHandler) PromoteVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Env       string `json:"env"`
		VersionID string `json:"version_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.svc.PromoteTeamVersion(
		r.Context(), chi.URLParam(r, "team_id"), middleware.GetWorkspaceIDFromContext(r.Context()), body.Env, body.VersionID,
	); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "promoted", "env": body.Env})
}

// RunByEnvRequest 按持久化团队 + env 运行。
type RunByEnvRequest struct {
	TeamID string `json:"team_id"`
	Env    string `json:"env"`
	Input  string `json:"input"`
}

// Run 按 team_id 和 env 拉取当前版本快照并执行协作。
func (h *TeamHandler) Run(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	var req RunByEnvRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	env := req.Env
	if env == "" {
		env = "dev"
	}
	spec, err := h.svc.GetRunSpec(r.Context(), req.TeamID, workspaceID, env)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	members := toRuntimeMembers(spec.Members)
	final, steps, runErr := h.runTeam(r.Context(), spec.Mode, members, req.Input, workspaceID, userID)
	if runErr != nil {
		http.Error(w, runErr.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"final": final, "steps": steps})
}

// AdHocRequest 临时编排请求(调试入口,不落库)。
type AdHocRequest struct {
	Mode    string        `json:"mode"`
	Members []team.Member `json:"members"`
	Input   string        `json:"input"`
}

// AdHoc 不落库、直接传 mode+members 跑一次(旧 /teams/runs 行为,留作调试)。
func (h *TeamHandler) AdHoc(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	var req AdHocRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	for _, member := range req.Members {
		if err := h.svc.ValidateAgentMember(
			r.Context(), workspaceID, member.AgentID, member.AgentVersionID,
		); err != nil {
			http.Error(w, "team member agent not found", http.StatusNotFound)
			return
		}
	}
	final, steps, err := h.runTeam(r.Context(), req.Mode, req.Members, req.Input, workspaceID, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"final": final, "steps": steps})
}

// runTeam 按 mode 跑 Supervisor / Pipeline(Run 与 AdHoc 共用)。
func (h *TeamHandler) runTeam(
	ctx context.Context, mode string, members []team.Member, input, workspaceID, userID string,
) (string, []team.Step, error) {
	run := func(ctx context.Context, m team.Member, in string) (string, error) {
		// m.AgentVersionID 来自团队快照 pin 的版本(注册团队 Run);ad-hoc 调试时为空 → 退化为当前版本。
		return h.runtime.Chat(ctx, engine.ChatStreamRequest{
			AgentID:        m.AgentID,
			AgentVersionID: m.AgentVersionID,
			WorkspaceID:    workspaceID,
			Message:        in,
			UserID:         userID,
		})
	}
	switch team.Mode(mode) {
	case team.ModePipeline:
		return team.RunPipeline(ctx, members, input, run)
	case team.ModeSupervisor:
		if len(members) < 2 {
			return "", nil, errSupervisorMembers
		}
		supervisor := engine.TeamMember{
			AgentID: members[0].AgentID, AgentVersionID: members[0].AgentVersionID, Role: members[0].Role,
		}
		workers := make([]engine.TeamMember, 0, len(members)-1)
		for _, member := range members[1:] {
			workers = append(workers, engine.TeamMember{
				AgentID: member.AgentID, AgentVersionID: member.AgentVersionID, Role: member.Role,
			})
		}
		final, runtimeSteps, err := h.runtime.RunSupervisorTeam(
			ctx, supervisor, workers, input, workspaceID, userID,
		)
		steps := make([]team.Step, len(runtimeSteps))
		for i, step := range runtimeSteps {
			steps[i] = team.Step{Role: step.Role, AgentID: step.AgentID, Input: step.Input, Output: step.Output}
		}
		return final, steps, err
	default:
		return "", nil, errUnknownMode
	}
}

func toRuntimeMembers(ms []domain.TeamMember) []team.Member {
	out := make([]team.Member, len(ms))
	for i, m := range ms {
		out[i] = team.Member{AgentID: m.AgentID, AgentVersionID: m.AgentVersionID, Role: m.Role}
	}
	return out
}

var (
	errSupervisorMembers = &teamError{"supervisor mode needs supervisor + at least 1 worker"}
	errUnknownMode       = &teamError{"unknown team mode"}
)

type teamError struct{ msg string }

func (e *teamError) Error() string { return e.msg }
