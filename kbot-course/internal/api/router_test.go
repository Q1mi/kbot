package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/iam"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

func TestProtectedWorkspaceContext(t *testing.T) {
	t.Parallel()

	service := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	user, err := service.Register(t.Context(), "student@example.com", "password123", "Student")
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(t.Context(), user.Email, "password123")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := service.ListUserWorkspaces(t.Context(), user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("default workspaces = %#v, err = %v", workspaces, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/context", bytes.NewReader(nil))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", workspaces[0].ID)
	response := httptest.NewRecorder()
	NewRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result map[string]string
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["user_id"] != user.ID || result["workspace_id"] != workspaces[0].ID || result["role"] != iam.WorkspaceRoleOwner {
		t.Fatalf("unexpected context: %#v", result)
	}
}

type completeChatRuntime struct{}

func (completeChatRuntime) ChatStream(_ context.Context, req engine.ChatRequest, emit engine.Emitter) error {
	if req.ConversationID == "" {
		return errors.New("conversation was not created")
	}
	if err := emit(engine.Event{Type: "answer_done", Text: "course reply"}); err != nil {
		return err
	}
	return emit(engine.Event{Type: "run_finished", Data: map[string]string{"status": "completed"}})
}

func TestFirstChatCreatesPinnedConversation(t *testing.T) {
	t.Parallel()

	iamService := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	user, err := iamService.Register(t.Context(), "first-chat@example.com", "password123", "First Chat")
	if err != nil {
		t.Fatal(err)
	}
	login, _ := iamService.Login(t.Context(), user.Email, "password123")
	workspaces, _ := iamService.ListUserWorkspaces(t.Context(), user.ID)
	agents := agent.NewService()
	router := NewRouterWithControlPlane(iamService, completeChatRuntime{}, ControlPlane{Agents: agents})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{"name":"support","template":"blank","system_prompt":"help","max_steps":4}`))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", workspaces[0].ID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create agent status = %d body = %s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+created.ID+"/chat", strings.NewReader(`{"message":"hello","agent_env":"dev"}`))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", workspaces[0].ID)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "conversation-") || !strings.Contains(response.Body.String(), "course reply") {
		t.Fatalf("first chat status = %d body = %s", response.Code, response.Body.String())
	}
	conversations := agents.ListConversations(t.Context(), workspaces[0].ID, created.ID)
	if len(conversations) != 1 || conversations[0].AgentVersionID == "" {
		t.Fatalf("conversations = %#v", conversations)
	}
}

func TestToolRegistryIsReachableThroughAuthenticatedWorkspaceAPI(t *testing.T) {
	t.Parallel()

	service := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	user, err := service.Register(t.Context(), "tools@example.com", "password123", "Tool Student")
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(t.Context(), user.Email, "password123")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := service.ListUserWorkspaces(t.Context(), user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("workspaces = %#v, err = %v", workspaces, err)
	}
	registry := platformtool.NewRegistry()
	router := NewRouterWithControlPlane(service, nil, ControlPlane{Tools: registry})
	body := `{"name":"get_order","source_type":"rest_api","description":"query order","schema_json":"{\"type\":\"object\"}","endpoint_config":"{\"url\":\"http://crossborder-sim:8091/api/orders\"}"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tools", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", workspaces[0].ID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := registry.List(t.Context(), workspaces[0].ID); len(got) != 1 || got[0].Name != "get_order" {
		t.Fatalf("registered tools = %#v", got)
	}
}

func TestProtectedWorkspaceRejectsUnrelatedHeader(t *testing.T) {
	t.Parallel()

	service := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	user, err := service.Register(t.Context(), "student2@example.com", "password123", "Student")
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(t.Context(), user.Email, "password123")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/context", nil)
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", "workspace-owned-by-someone-else")
	response := httptest.NewRecorder()
	NewRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}
