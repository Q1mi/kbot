package v1

import (
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/go-chi/chi/v5"
)

// PromptHandler Prompt 中心处理器
type PromptHandler struct {
	svc *prompt.Service
}

// NewPromptHandler 创建 Prompt 处理器
func NewPromptHandler(svc *prompt.Service) *PromptHandler {
	return &PromptHandler{svc: svc}
}

// CreatePrompt 创建 Prompt（含 v1，默认绑定 dev）
// @Summary  创建 Prompt(含 v1)
// @Tags     prompts
// @Security BearerAuth
// @Param    body  body      prompt.CreatePromptRequest  true  "Prompt"
// @Success  201   {object}  map[string]interface{}
// @Router   /prompts [post]
func (h *PromptHandler) CreatePrompt(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())

	var req prompt.CreatePromptRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.CreatedBy = userID
	req.WorkspaceID = workspaceID

	p, v, err := h.svc.CreatePrompt(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"prompt": p, "version": v})
}

// CreateVersion 新增 immutable 版本
func (h *PromptHandler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	userID := middleware.GetUserIDFromContext(r.Context())

	var body struct {
		Template              string                  `json:"template"`
		VariablesSchema       string                  `json:"variables_schema"`
		ModelProfileVersionID string                  `json:"model_profile_version_id"`
		GenerationConfig      domain.GenerationConfig `json:"generation_config"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	v, err := h.svc.CreateVersionConfigured(r.Context(), promptID, body.Template, body.VariablesSchema,
		body.ModelProfileVersionID, body.GenerationConfig, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *PromptHandler) StartRollout(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	var body struct {
		Env                string `json:"env"`
		CandidateVersionID string `json:"candidate_version_id"`
		TrafficPercent     int    `json:"traffic_percent"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	exp, err := h.svc.StartRollout(r.Context(), promptID, body.Env, body.CandidateVersionID,
		body.TrafficPercent, middleware.GetUserIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, exp)
}

func (h *PromptHandler) UpdateRolloutTraffic(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	var body struct {
		Env            string `json:"env"`
		TrafficPercent int    `json:"traffic_percent"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	exp, err := h.svc.UpdateRolloutTraffic(r.Context(), promptID, body.Env, body.TrafficPercent,
		middleware.GetUserIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, exp)
}

func (h *PromptHandler) CompleteRollout(w http.ResponseWriter, r *http.Request) {
	h.finishRollout(w, r, true)
}

func (h *PromptHandler) RollbackRollout(w http.ResponseWriter, r *http.Request) {
	h.finishRollout(w, r, false)
}

func (h *PromptHandler) finishRollout(w http.ResponseWriter, r *http.Request, complete bool) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	var body struct {
		Env string `json:"env"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var err error
	if complete {
		err = h.svc.CompleteRollout(r.Context(), promptID, body.Env,
			middleware.GetUserIDFromContext(r.Context()))
	} else {
		err = h.svc.RollbackRollout(r.Context(), promptID, body.Env,
			middleware.GetUserIDFromContext(r.Context()))
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Promote dev→staging→prod 晋升 / 首次发布（改 env 指针）
// @Summary  晋升 Prompt 版本到某环境
// @Tags     prompts
// @Security BearerAuth
// @Param    prompt_id  path      string  true  "Prompt ID"
// @Success  200        {object}  map[string]string
// @Router   /prompts/{prompt_id}/promote [post]
func (h *PromptHandler) Promote(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	var body struct {
		Env       string `json:"env"`
		VersionID string `json:"version_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.svc.Promote(r.Context(), promptID, body.Env, body.VersionID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "promoted"})
}

// Rollback 回滚 env 指针
func (h *PromptHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	var body struct {
		Env       string `json:"env"`
		VersionID string `json:"version_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.svc.Rollback(r.Context(), promptID, body.Env, body.VersionID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled_back"})
}

// Diff 语义级 diff（from→to）
func (h *PromptHandler) Diff(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	from, err := queryInt(r, "from", 0, 1, int(^uint(0)>>1))
	if err != nil || from == 0 {
		http.Error(w, "from must be a positive integer", http.StatusBadRequest)
		return
	}
	to, err := queryInt(r, "to", 0, 1, int(^uint(0)>>1))
	if err != nil || to == 0 {
		http.Error(w, "to must be a positive integer", http.StatusBadRequest)
		return
	}
	diff, err := h.svc.Diff(r.Context(), promptID, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

// Render 渲染（解析 env 指针 / A-B → 模板渲染）
func (h *PromptHandler) Render(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	userID := middleware.GetUserIDFromContext(r.Context())
	var body struct {
		Env  string         `json:"env"`
		Vars map[string]any `json:"vars"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Env == "" {
		body.Env = prompt.EnvDev
	}
	text, err := h.svc.Render(r.Context(), promptID, body.Env, userID, body.Vars)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"rendered": text})
}

// StartExperiment 配置 A/B 实验
func (h *PromptHandler) StartExperiment(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	var body struct {
		Env      string                     `json:"env"`
		Variants []domain.ExperimentVariant `json:"variants"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	exp, err := h.svc.StartExperiment(r.Context(), promptID, body.Env, body.Variants)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, exp)
}

// ListPrompts 列出 Prompt
// @Summary  列出 Prompt
// @Tags     prompts
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /prompts [get]
func (h *PromptHandler) ListPrompts(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	prompts, err := h.svc.ListPrompts(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, prompts)
}

// ListVersions 列出版本
func (h *PromptHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	promptID := chi.URLParam(r, "prompt_id")
	if !h.ensurePromptWorkspace(w, r, promptID) {
		return
	}
	versions, err := h.svc.ListVersions(r.Context(), promptID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (h *PromptHandler) ensurePromptWorkspace(w http.ResponseWriter, r *http.Request, promptID string) bool {
	if err := h.svc.EnsurePromptWorkspace(
		r.Context(), promptID, middleware.GetWorkspaceIDFromContext(r.Context()),
	); err != nil {
		http.Error(w, "prompt not found", http.StatusNotFound)
		return false
	}
	return true
}
