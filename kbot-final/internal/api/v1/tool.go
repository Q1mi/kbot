package v1

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/go-chi/chi/v5"
)

// ToolHandler Tool Registry 处理器
type ToolHandler struct {
	toolService *tool.Service
	registry    *tooling.Registry
}

// NewToolHandler 创建 Tool 处理器
func NewToolHandler(toolService *tool.Service, registry *tooling.Registry) *ToolHandler {
	return &ToolHandler{toolService: toolService, registry: registry}
}

// CreateTool 注册工具
// @Summary  注册工具
// @Tags     tools
// @Security BearerAuth
// @Param    body  body      tool.CreateToolRequest  true  "工具定义"
// @Success  201   {object}  map[string]interface{}
// @Router   /tools [post]
func (h *ToolHandler) CreateTool(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())

	var req tool.CreateToolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.CreatedBy = userID
	req.WorkspaceID = workspaceID

	t, err := h.toolService.CreateTool(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// ListTools 列出工具
// @Summary  列出工具
// @Tags     tools
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /tools [get]
func (h *ToolHandler) ListTools(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	tools, err := h.toolService.ListTools(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tools)
}

// ListToolVersions 列出 Tool 的全部不可变版本。
// @Summary  列出 Tool 不可变版本
// @Tags     tools
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /tools/{tool_id}/versions [get]
func (h *ToolHandler) ListToolVersions(w http.ResponseWriter, r *http.Request) {
	if !h.ensureToolWorkspace(w, r) {
		return
	}
	versions, err := h.toolService.ListToolVersions(r.Context(), chi.URLParam(r, "tool_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// CreateToolVersion 基于当前配置创建新 draft 版本。
// @Summary  创建 Tool 草稿版本
// @Tags     tools
// @Security BearerAuth
// @Param    body  body      tool.CreateToolVersionRequest  true  "版本配置"
// @Success  201   {object}  map[string]interface{}
// @Router   /tools/{tool_id}/versions [post]
func (h *ToolHandler) CreateToolVersion(w http.ResponseWriter, r *http.Request) {
	if !h.ensureToolWorkspace(w, r) {
		return
	}
	var req tool.CreateToolVersionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.CreatedBy = middleware.GetUserIDFromContext(r.Context())
	version, err := h.toolService.CreateToolVersion(r.Context(), chi.URLParam(r, "tool_id"), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

// TestTool Sandbox 试调工具：真正构造执行器调用一次，并把结果落账（供 Publish 门禁）。
func (h *ToolHandler) TestTool(w http.ResponseWriter, r *http.Request) {
	toolID := chi.URLParam(r, "tool_id")
	if !h.ensureToolWorkspace(w, r) {
		return
	}
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	bt, err := h.registry.Build(r.Context(), toolID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	start := time.Now()
	output, execErr := bt.Executor.Execute(r.Context(), body.Input)
	latency := int(time.Since(start).Milliseconds())
	status := "success"
	if execErr != nil {
		status = "error"
	}

	run, err := h.toolService.RecordTestRun(r.Context(), toolID, string(body.Input), output, status, latency, execErr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// PublishTool 发布工具
func (h *ToolHandler) PublishTool(w http.ResponseWriter, r *http.Request) {
	toolID := chi.URLParam(r, "tool_id")
	if !h.ensureToolWorkspace(w, r) {
		return
	}
	if err := h.toolService.PublishTool(r.Context(), toolID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// PublishToolVersion 发布 URL 指定的 Tool 版本。
// @Summary  发布指定 Tool 版本
// @Tags     tools
// @Security BearerAuth
// @Success  200  {object}  map[string]string
// @Router   /tools/{tool_id}/versions/{version_id}/publish [post]
func (h *ToolHandler) PublishToolVersion(w http.ResponseWriter, r *http.Request) {
	if !h.ensureToolWorkspace(w, r) {
		return
	}
	if err := h.toolService.PublishToolVersion(
		r.Context(), chi.URLParam(r, "tool_id"), chi.URLParam(r, "version_id"),
	); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (h *ToolHandler) ensureToolWorkspace(w http.ResponseWriter, r *http.Request) bool {
	if err := h.toolService.EnsureToolWorkspace(
		r.Context(), chi.URLParam(r, "tool_id"), middleware.GetWorkspaceIDFromContext(r.Context()),
	); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return false
	}
	return true
}
