package v1

import (
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/go-chi/chi/v5"
)

// KBHandler 知识库处理器
type KBHandler struct {
	kbService *kb.Service
}

// NewKBHandler 创建 KB 处理器
func NewKBHandler(kbService *kb.Service) *KBHandler {
	return &KBHandler{kbService: kbService}
}

// CreateKB 创建知识库
// @Summary  创建知识库
// @Tags     kbs
// @Security BearerAuth
// @Param    body  body      kb.CreateKBRequest  true  "知识库"
// @Success  201   {object}  map[string]interface{}
// @Router   /kbs [post]
func (h *KBHandler) CreateKB(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())

	var req kb.CreateKBRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.CreatedBy = userID
	req.WorkspaceID = workspaceID

	k, err := h.kbService.CreateKB(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, k)
}

// ListKBs 列出知识库
// @Summary  列出知识库
// @Tags     kbs
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /kbs [get]
func (h *KBHandler) ListKBs(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	kbs, err := h.kbService.ListKBs(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, kbs)
}

// SyncConnectorRequest 同步连接器请求
type SyncConnectorRequest struct {
	RootPath string `json:"root_path"`
}

// SyncConnector 触发本地 Markdown 文件夹同步并 ingest
func (h *KBHandler) SyncConnector(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "kb_id")
	if !h.ensureKBWorkspace(w, r, kbID) {
		return
	}
	var req SyncConnectorRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.kbService.SyncMarkdownFolderAllowed(r.Context(), kbID, req.RootPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// SearchRequest 检索请求。Mode 可选:bm25 / vector / hybrid(默认 hybrid)。
type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
	Mode  string `json:"mode"`
}

// Search 提供 bm25、vector 和 hybrid 三种检索模式。
// @Summary  KB 检索(bm25/vector/hybrid)
// @Tags     kbs
// @Security BearerAuth
// @Param    kb_id  path      string         true  "KB ID"
// @Param    body   body      SearchRequest  true  "查询 + 模式 + top_k"
// @Success  200    {array}   map[string]interface{}
// @Router   /kbs/{kb_id}/search [post]
func (h *KBHandler) Search(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "kb_id")
	if !h.ensureKBWorkspace(w, r, kbID) {
		return
	}
	var req SearchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TopK == 0 {
		req.TopK = 5
	}
	if req.Mode == "" {
		req.Mode = "hybrid"
	}
	passages, err := h.kbService.SearchMode(r.Context(), kbID, req.Query, req.TopK, req.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, passages)
}

// ListDocuments 列出知识库文档。
func (h *KBHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "kb_id")
	if !h.ensureKBWorkspace(w, r, kbID) {
		return
	}
	docs, err := h.kbService.ListDocuments(r.Context(), kbID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

// ListConnectors 列出知识库连接器实例。
func (h *KBHandler) ListConnectors(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "kb_id")
	if !h.ensureKBWorkspace(w, r, kbID) {
		return
	}
	conns, err := h.kbService.ListConnectors(r.Context(), kbID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, conns)
}

// ListIngestJobs 查看 ingest 状态
func (h *KBHandler) ListIngestJobs(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "kb_id")
	if !h.ensureKBWorkspace(w, r, kbID) {
		return
	}
	jobs, err := h.kbService.ListIngestJobs(r.Context(), kbID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (h *KBHandler) ensureKBWorkspace(w http.ResponseWriter, r *http.Request, kbID string) bool {
	if err := h.kbService.EnsureKBWorkspace(
		r.Context(), kbID, middleware.GetWorkspaceIDFromContext(r.Context()),
	); err != nil {
		http.Error(w, "knowledge base not found", http.StatusNotFound)
		return false
	}
	return true
}
