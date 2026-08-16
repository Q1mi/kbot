package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"

	"github.com/Q1mi/kbot/internal/a2ui"
	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
)

// ApprovalHandler 处理人在环审批队列。
type ApprovalHandler struct {
	store approval.Store
	jobs  *jobs.Client
	audit *audit.Service
}

func NewApprovalHandler(store approval.Store, jobsClient *jobs.Client, auditServices ...*audit.Service) *ApprovalHandler {
	h := &ApprovalHandler{store: store, jobs: jobsClient}
	if len(auditServices) > 0 {
		h.audit = auditServices[0]
	}
	return h
}

// List 返回待审批队列。
// @Summary  待审批队列
// @Tags     approvals
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /approvals [get]
func (h *ApprovalHandler) List(w http.ResponseWriter, r *http.Request) {
	pend, err := h.store.ListPending(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, pend)
}

// Approve 批准一条审批:标 approved → enqueue engine_resume 让 worker 拉 checkpoint 续跑。
// @Summary  批准审批(触发续跑)
// @Tags     approvals
// @Security BearerAuth
// @Param    approval_id  path      string  true  "审批 ID"
// @Success  200          {object}  map[string]interface{}
// @Router   /approvals/{approval_id}/approve [post]
func (h *ApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "approval_id")
	approverID := middleware.GetUserIDFromContext(r.Context())
	appr, err := h.resolve(r.Context(), id, approval.StatusApproved, approverID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": approval.StatusApproved, "conversation_id": appr.ConversationID})
}

// Reject 拒绝一条审批(不续跑)。
func (h *ApprovalHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "approval_id")
	approverID := middleware.GetUserIDFromContext(r.Context())
	if _, err := h.resolve(r.Context(), id, approval.StatusRejected, approverID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": approval.StatusRejected})
}

// A2UIAction 接收符合 v0.9.1 client_to_server schema 的受控 UI action。
// @Summary  处理 A2UI 审批动作
// @Tags     conversations
// @Security BearerAuth
// @Router   /conversations/{conversation_id}/a2ui/actions [post]
func (h *ApprovalHandler) A2UIAction(w http.ResponseWriter, r *http.Request) {
	conversationID := chi.URLParam(r, "conversation_id")
	var message a2ui.ClientMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&message); err != nil {
		http.Error(w, "invalid A2UI action body", http.StatusBadRequest)
		return
	}
	if err := a2ui.ValidateClientMessage(message); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if message.Action == nil {
		http.Error(w, "A2UI error reports are not executable actions", http.StatusBadRequest)
		return
	}
	approvalID, ok := message.Action.Context["approval_id"].(string)
	if !ok || approvalID == "" {
		http.Error(w, "action context requires approval_id", http.StatusBadRequest)
		return
	}
	contextConversationID, ok := message.Action.Context["conversation_id"].(string)
	if !ok || !sameResourceID(contextConversationID, conversationID) {
		http.Error(w, "action conversation does not match route", http.StatusForbidden)
		return
	}
	if message.Action.SurfaceID != "approval-"+approvalID {
		http.Error(w, "action surface does not match approval", http.StatusForbidden)
		return
	}

	status := approval.StatusRejected
	if message.Action.Name == a2ui.ActionApprove {
		status = approval.StatusApproved
	}
	appr, err := h.store.Get(r.Context(), approvalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !sameResourceID(appr.ConversationID, conversationID) {
		http.Error(w, "approval does not belong to conversation", http.StatusForbidden)
		return
	}
	approverID := middleware.GetUserIDFromContext(r.Context())
	if _, err := h.resolveKnown(r.Context(), appr, status, approverID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", a2ui.MIMEType)
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	for _, response := range a2ui.ApprovalStatusMessages(approvalID, status) {
		if err := a2ui.ValidateMessage(response); err != nil {
			return
		}
		_ = encoder.Encode(response)
	}
}

// PostgreSQL UUID 会回读成带连字符形式，运行时生成事件时保留 32 位 hex。
func sameResourceID(left, right string) bool {
	normalize := func(value string) string {
		return strings.ToLower(strings.ReplaceAll(value, "-", ""))
	}
	return normalize(left) == normalize(right)
}

func (h *ApprovalHandler) resolve(ctx context.Context, id, status, approverID string) (*approval.Approval, error) {
	appr, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return h.resolveKnown(ctx, appr, status, approverID)
}

func (h *ApprovalHandler) resolveKnown(ctx context.Context, appr *approval.Approval, status, approverID string) (*approval.Approval, error) {
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	if appr.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("approval not found")
	}
	resolved, err := h.store.ResolvePending(ctx, appr.ID, workspaceID, status, approverID)
	if err != nil {
		return nil, err
	}
	if status == approval.StatusApproved && h.jobs != nil && resolved.ConversationID != "" {
		payload, _ := json.Marshal(jobs.ResumePayload{ConversationID: resolved.ConversationID, ApprovalID: resolved.ID})
		if _, err := h.jobs.Enqueue(
			asynq.NewTask(jobs.TypeEngineResume, payload),
			asynq.TaskID("approval-resume-"+strings.ReplaceAll(resolved.ID, "-", "")),
		); err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil, fmt.Errorf("enqueue resume: %w", err)
		}
	}
	if h.audit != nil {
		h.audit.RecordWorkspace(workspaceID, approverID, "approval_"+status, "approval", resolved.ID)
	}
	return resolved, nil
}
