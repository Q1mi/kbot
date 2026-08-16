package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/approval"
)

func a2uiActionRequest(body, conversationID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/conversations/"+conversationID+"/a2ui/actions", strings.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("conversation_id", conversationID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, route)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "approver-1")
	ctx = context.WithValue(ctx, middleware.ContextKeyWorkspaceID, "w1")
	return req.WithContext(ctx)
}

func TestApprovalHandlerA2UIActionApproves(t *testing.T) {
	store := approval.NewMemoryStore()
	if err := store.CreatePending(context.Background(), &approval.Approval{
		ID: "ap-1", WorkspaceID: "w1", ConversationID: "conv-1", Status: approval.StatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewApprovalHandler(store, nil)
	body := `{"version":"v0.9","action":{"name":"approval.approve","surfaceId":"approval-ap-1","sourceComponentId":"approve-action","timestamp":"2026-07-25T10:00:00Z","context":{"approval_id":"ap-1","conversation_id":"conv-1"}}}`
	recorder := httptest.NewRecorder()
	handler.A2UIAction(recorder, a2uiActionRequest(body, "conv-1"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/a2ui+json" {
		t.Fatalf("unexpected content type %q", got)
	}
	if !strings.Contains(recorder.Body.String(), `"path":"/status"`) {
		t.Fatalf("missing status data-model update: %s", recorder.Body.String())
	}
	resolved, _ := store.Get(context.Background(), "ap-1")
	if resolved.Status != approval.StatusApproved || resolved.ApproverID != "approver-1" {
		t.Fatalf("approval was not resolved: %+v", resolved)
	}
}

func TestApprovalHandlerA2UIActionRejectsConversationTampering(t *testing.T) {
	store := approval.NewMemoryStore()
	_ = store.CreatePending(context.Background(), &approval.Approval{
		ID: "ap-1", WorkspaceID: "w1", ConversationID: "conv-1", Status: approval.StatusPending,
	})
	handler := NewApprovalHandler(store, nil)
	body := `{"version":"v0.9","action":{"name":"approval.approve","surfaceId":"approval-ap-1","sourceComponentId":"approve-action","timestamp":"2026-07-25T10:00:00Z","context":{"approval_id":"ap-1","conversation_id":"conv-2"}}}`
	recorder := httptest.NewRecorder()
	handler.A2UIAction(recorder, a2uiActionRequest(body, "conv-2"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
	pending, _ := store.Get(context.Background(), "ap-1")
	if pending.Status != approval.StatusPending {
		t.Fatalf("tampered action changed status to %q", pending.Status)
	}
}

func TestSameResourceIDAcceptsPostgresUUIDFormatting(t *testing.T) {
	if !sameResourceID("367c41ba-594a-38ac-2628-4e7ee344f124", "367c41ba594a38ac26284e7ee344f124") {
		t.Fatal("expected canonical and compact UUIDs to match")
	}
}
