package v1

import (
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/guardconfig"
	"github.com/go-chi/chi/v5"
)

type GuardHandler struct{ svc *guardconfig.Service }

func NewGuardHandler(svc *guardconfig.Service) *GuardHandler { return &GuardHandler{svc: svc} }

// ListRules 返回当前工作空间 Guard 规则。
// @Summary  列出 Guard 规则
// @Tags     guard
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /guard/rules [get]
func (h *GuardHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.svc.ListRules(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

// CreateRule 创建动态 Guard 规则。
// @Summary  创建 Guard 规则
// @Tags     guard
// @Security BearerAuth
// @Success  201  {object}  map[string]interface{}
// @Router   /guard/rules [post]
func (h *GuardHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var body guardconfig.Rule
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	rule, err := h.svc.CreateRule(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// UpdateRule 更新或启停动态 Guard 规则。
// @Summary  更新 Guard 规则
// @Tags     guard
// @Security BearerAuth
// @Success  200  {object}  map[string]interface{}
// @Router   /guard/rules/{rule_id} [put]
func (h *GuardHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	var body guardconfig.Rule
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	rule, err := h.svc.UpdateRule(
		r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()), chi.URLParam(r, "rule_id"), body,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// ListQuotas 返回当前周期配额。
// @Summary  列出 Guard 配额
// @Tags     guard
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /guard/quotas [get]
func (h *GuardHandler) ListQuotas(w http.ResponseWriter, r *http.Request) {
	quotas, err := h.svc.ListQuotas(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, quotas)
}

// SetQuota 设置当前周期的 Guard 配额上限。
// @Summary  设置 Guard 配额
// @Tags     guard
// @Security BearerAuth
// @Success  200  {object}  map[string]interface{}
// @Router   /guard/quotas/{metric} [put]
func (h *GuardHandler) SetQuota(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Limit int64 `json:"limit"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	quota, err := h.svc.SetQuota(
		r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()), chi.URLParam(r, "metric"), body.Limit,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, quota)
}
