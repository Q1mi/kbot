package v1

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/audit"
)

// ObjectExporter 是审计导出落对象存储的最小接口(objstore.Client 满足之)。
type ObjectExporter interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	PresignedGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// AuditHandler 审计检索处理器
type AuditHandler struct {
	svc      *audit.Service
	exporter ObjectExporter // 可为 nil(未配置对象存储时 /exports 返回 503)
}

// NewAuditHandler 创建审计处理器
func NewAuditHandler(svc *audit.Service, exporter ObjectExporter) *AuditHandler {
	return &AuditHandler{svc: svc, exporter: exporter}
}

// Logs 按 conversation_id / actor 检索审计日志
// @Summary  检索审计日志
// @Tags     audit
// @Security BearerAuth
// @Param    conversation_id  query     string  false  "会话 ID"
// @Param    actor            query     string  false  "操作者"
// @Param    limit            query     int     false  "条数"
// @Success  200              {array}   map[string]interface{}
// @Router   /audit/logs [get]
func (h *AuditHandler) Logs(w http.ResponseWriter, r *http.Request) {
	limit, err := queryInt(r, "limit", 100, 1, 1000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logs, err := h.svc.Query(r.Context(), audit.QueryFilter{
		WorkspaceID:    middleware.GetWorkspaceIDFromContext(r.Context()),
		ConversationID: r.URL.Query().Get("conversation_id"),
		Actor:          r.URL.Query().Get("actor"),
		Limit:          limit,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// InjectionLogs 返回 Guard 拦截记录（resource_type=guard，§15.4）
// @Summary  Guard 注入/拦截日志
// @Tags     guard
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /guard/injection-logs [get]
func (h *AuditHandler) InjectionLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.svc.Query(r.Context(), audit.QueryFilter{
		WorkspaceID: middleware.GetWorkspaceIDFromContext(r.Context()), ResourceType: "guard", Limit: 200,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// Export 把某会话的审计轨迹导成 CSV，上传对象存储并返回预签名 URL。
func (h *AuditHandler) Export(w http.ResponseWriter, r *http.Request) {
	if h.exporter == nil {
		http.Error(w, "object store not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		ConversationID string `json:"conversation_id"`
		Format         string `json:"format"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ConversationID == "" {
		http.Error(w, "conversation_id required", http.StatusBadRequest)
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format != "" && format != "csv" {
		http.Error(w, "format must be csv", http.StatusBadRequest)
		return
	}

	logs, err := h.svc.Query(r.Context(), audit.QueryFilter{
		WorkspaceID: middleware.GetWorkspaceIDFromContext(r.Context()), ConversationID: req.ConversationID, Limit: 100000,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"id", "actor", "action", "resource_type", "resource_id", "created_at"})
	for _, l := range logs {
		_ = cw.Write([]string{l.ID, l.Actor, l.Action, l.ResourceType, l.ResourceID, l.CreatedAt.Format(time.RFC3339)})
	}
	cw.Flush()

	key := fmt.Sprintf("exports/audit-%s-%d.csv", req.ConversationID, time.Now().UTC().Unix())
	if err := h.exporter.Put(r.Context(), key, &buf, int64(buf.Len()), "text/csv"); err != nil {
		http.Error(w, "upload export: "+err.Error(), http.StatusInternalServerError)
		return
	}
	url, err := h.exporter.PresignedGet(r.Context(), key, time.Hour)
	if err != nil {
		http.Error(w, "presign: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "key": key, "count": len(logs)})
}
