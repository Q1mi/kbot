package v1

import (
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/go-chi/chi/v5"
)

// SkillHandler 技能处理器
type SkillHandler struct {
	svc    *skill.Service
	agents *agent.Service
}

// NewSkillHandler 创建技能处理器
func NewSkillHandler(svc *skill.Service, agents *agent.Service) *SkillHandler {
	return &SkillHandler{svc: svc, agents: agents}
}

// CreateSkill 从 SKILL.md 创建技能（含 v1 draft）
// @Summary  从 SKILL.md 创建技能
// @Tags     skills
// @Security BearerAuth
// @Param    body  body      skill.CreateSkillRequest  true  "SKILL.md"
// @Success  201   {object}  map[string]interface{}
// @Router   /skills [post]
func (h *SkillHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())

	var req skill.CreateSkillRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.CreatedBy = userID
	req.WorkspaceID = workspaceID

	sk, v, err := h.svc.CreateSkill(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skill": sk, "version": v})
}

// CreateSkillVersionRequest 新建 Skill 版本请求。
type CreateSkillVersionRequest struct {
	SkillMD string `json:"skill_md"`
}

// CreateVersion 从新的 SKILL.md 新增版本
// @Summary  创建 Skill 不可变版本
// @Tags     skills
// @Security BearerAuth
// @Param    skill_id  path      string                     true  "Skill ID"
// @Param    body      body      CreateSkillVersionRequest  true  "SKILL.md"
// @Success  201       {object}  map[string]interface{}
// @Router   /skills/{skill_id}/versions [post]
func (h *SkillHandler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skill_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	if err := h.svc.EnsureSkillWorkspace(r.Context(), skillID, workspaceID); err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	userID := middleware.GetUserIDFromContext(r.Context())
	var body CreateSkillVersionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	v, err := h.svc.CreateVersion(r.Context(), skillID, body.SkillMD, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// ListVersions 列出 Skill 的全部不可变版本。
// @Summary  列出 Skill 不可变版本
// @Tags     skills
// @Security BearerAuth
// @Param    skill_id  path      string  true  "Skill ID"
// @Success  200       {array}   map[string]interface{}
// @Router   /skills/{skill_id}/versions [get]
func (h *SkillHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skill_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	versions, err := h.svc.ListVersions(r.Context(), skillID, workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// Publish 发布技能版本（走强制校验）
func (h *SkillHandler) Publish(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skill_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	var body struct {
		VersionID string `json:"version_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.svc.ValidateVersion(r.Context(), skillID, body.VersionID, workspaceID, false); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := h.svc.Publish(r.Context(), body.VersionID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// Subscribe 把技能版本订阅到 Agent
func (h *SkillHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skill_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	userID := middleware.GetUserIDFromContext(r.Context())
	var body struct {
		VersionID string `json:"version_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if h.agents == nil {
		http.Error(w, "agent version service is not configured", http.StatusInternalServerError)
		return
	}
	if err := h.svc.ValidateVersion(r.Context(), skillID, body.VersionID, workspaceID, true); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	agentVersion, err := h.agents.AttachSkillVersion(r.Context(), body.AgentID, workspaceID, body.VersionID, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.Subscribe(r.Context(), skillID, body.VersionID, body.AgentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "subscribed", "agent_version": agentVersion})
}

// ListSkills 列出技能
// @Summary  列出技能
// @Tags     skills
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /skills [get]
func (h *SkillHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	skills, err := h.svc.ListSkills(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, skills)
}
