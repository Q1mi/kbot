package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	pagent "github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/util"
)

func withCtx(r *http.Request, userID, workspaceID string) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, middleware.ContextKeyWorkspaceID, workspaceID)
	return r.WithContext(ctx)
}

func TestAgentHandler_ListAndGet(t *testing.T) {
	store := platform.NewMemoryAgentStore()
	ctx := context.Background()
	_ = store.CreateAgent(ctx, &domain.Agent{ID: "ag1", WorkspaceID: "w1", Name: "退款助手", Template: "customer_support"})
	_ = store.CreateAgent(ctx, &domain.Agent{ID: "ag2", WorkspaceID: "w2", Name: "别的空间", Template: "custom"})

	h := NewAgentHandler(pagent.NewService(store, nil, nil, nil), nil)

	// ListAgents 只返回当前 workspace 的。
	rec := httptest.NewRecorder()
	h.ListAgents(rec, withCtx(httptest.NewRequest(http.MethodGet, "/agents", nil), "u1", "w1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var agents []domain.Agent
	if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].ID != "ag1" {
		t.Fatalf("want only ag1 in w1, got %+v", agents)
	}

	// GetAgent by id.
	rec = httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agent_id", "ag1")
	req := httptest.NewRequest(http.MethodGet, "/agents/ag1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withCtx(req, "u1", "w1")
	h.GetAgent(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "退款助手") {
		t.Fatalf("get agent failed: %d %s", rec.Code, rec.Body.String())
	}

	// 不存在 → 404。
	rec = httptest.NewRecorder()
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("agent_id", "nope")
	req2 := httptest.NewRequest(http.MethodGet, "/agents/nope", nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx2))
	req2 = withCtx(req2, "u1", "w1")
	h.GetAgent(rec, req2)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for missing agent, got %d", rec.Code)
	}

	// 其他 Workspace 的资源 ID 不能作为访问凭据。
	rec = httptest.NewRecorder()
	rctx3 := chi.NewRouteContext()
	rctx3.URLParams.Add("agent_id", "ag2")
	req3 := httptest.NewRequest(http.MethodGet, "/agents/ag2", nil)
	req3 = req3.WithContext(context.WithValue(req3.Context(), chi.RouteCtxKey, rctx3))
	req3 = withCtx(req3, "u1", "w1")
	h.GetAgent(rec, req3)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-workspace agent, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentHandler_GetConversationRestoresTraceAndA2UI(t *testing.T) {
	store := platform.NewMemoryAgentStore()
	approvalStore := approval.NewMemoryStore()
	ctx := context.Background()
	conversationID := util.GenerateID()
	runtimeJSON := `{"latest_trace_id":"trace-history-123","system_prompt":"course prompt"}`
	if err := store.CreateConversation(ctx, &domain.Conversation{
		ID: conversationID, AgentID: "agent-1", AgentVersionID: "version-1",
		WorkspaceID: "workspace-1", UserID: "user-1", RuntimeConfigJSON: runtimeJSON,
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	approvalID := util.GenerateID()
	if err := approvalStore.CreatePending(ctx, &approval.Approval{
		ID: approvalID, ConversationID: conversationID, Action: "approve_refund",
		Payload: `{"order_id":"TTS-1001","amount":20}`,
	}); err != nil {
		t.Fatal(err)
	}
	createdApproval, err := approvalStore.Get(ctx, approvalID)
	if err != nil {
		t.Fatal(err)
	}
	userMessageAt := createdApproval.CreatedAt.Add(time.Millisecond)
	if err := store.CreateMessage(ctx, &domain.Message{
		ID: util.GenerateID(), ConversationID: conversationID, Role: "user",
		Content: "申请退款", CreatedAt: userMessageAt,
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewAgentHandler(pagent.NewService(store, nil, nil, nil), nil, approvalStore)
	recorder := httptest.NewRecorder()
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("conversation_id", conversationID)
	request := httptest.NewRequest(http.MethodGet, "/conversations/"+conversationID, nil)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	request = withCtx(request, "user-1", "workspace-1")
	handler.GetConversation(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		TraceID      string                  `json:"trace_id"`
		A2UISurfaces []historicalA2UISurface `json:"a2ui_surfaces"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.TraceID != "trace-history-123" {
		t.Fatalf("trace_id=%q", response.TraceID)
	}
	if len(response.A2UISurfaces) != 1 || len(response.A2UISurfaces[0].Messages) < 3 ||
		response.A2UISurfaces[0].Messages[0].CreateSurface == nil {
		t.Fatalf("a2ui surfaces=%+v", response.A2UISurfaces)
	}
	if !response.A2UISurfaces[0].CreatedAt.After(userMessageAt) {
		t.Fatalf("surface should follow triggering user message: surface=%s user=%s",
			response.A2UISurfaces[0].CreatedAt, userMessageAt)
	}
}

func TestIAMHandler_ListUsersAndCreateWorkspace(t *testing.T) {
	store := platform.NewMemoryIAMStore()
	svc := iam.NewService(store, []byte("test-key"))
	h := NewIAMHandler(svc)
	ctx := context.Background()
	user, err := svc.CreateUser(ctx, "a@x.io", "pw123456", "Alice")
	if err != nil {
		t.Fatal(err)
	}

	// ListUsers.
	rec := httptest.NewRecorder()
	h.ListUsers(rec, httptest.NewRequest(http.MethodGet, "/users", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "a@x.io") {
		t.Fatalf("list users failed: %d %s", rec.Code, rec.Body.String())
	}

	// CreateWorkspace → 201,然后 ListWorkspaces 能看到。
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"name":"客服部","description":"d"}`)
	h.CreateWorkspace(rec, withCtx(httptest.NewRequest(http.MethodPost, "/workspaces", body), user.ID, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ListWorkspaces(rec, withCtx(httptest.NewRequest(http.MethodGet, "/workspaces", nil), user.ID, ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "客服部") {
		t.Fatalf("list workspaces failed: %d %s", rec.Code, rec.Body.String())
	}

	// CreateWorkspace 缺 name → 400。
	rec = httptest.NewRecorder()
	h.CreateWorkspace(rec, withCtx(httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader(`{}`)), user.ID, ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty name, got %d", rec.Code)
	}
}
