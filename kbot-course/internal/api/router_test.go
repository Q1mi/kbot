package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	platformeval "github.com/Q1mi/kbot/internal/platform/eval"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	platformteam "github.com/Q1mi/kbot/internal/platform/team"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/guard"
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

func TestCodeExecutionEndpointAcceptsOnlySupportedLanguage(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: `{"language":"python"}`, want: "python", ok: true},
		{raw: `{"language":"bash"}`, want: "bash", ok: true},
		{raw: `{"language":"ruby"}`, ok: false},
	} {
		got, err := executableToolEndpoint("code_execution", test.raw)
		if test.ok && (err != nil || got != test.want) {
			t.Fatalf("endpoint(%s) = %q, %v", test.raw, got, err)
		}
		if !test.ok && err == nil {
			t.Fatalf("endpoint(%s) should fail", test.raw)
		}
	}
}

func TestReadinessIncludesSandboxRunner(t *testing.T) {
	service := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	router := NewRouterWithControlPlane(service, nil, ControlPlane{Readiness: func(context.Context) error {
		return errors.New("sandbox runner unavailable")
	}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "sandbox runner unavailable") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
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

func TestTeamAPIPinsVersionsAndRunsPipeline(t *testing.T) {
	t.Parallel()
	iamService := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	user, err := iamService.Register(t.Context(), "team@example.com", "password123", "Team Student")
	if err != nil {
		t.Fatal(err)
	}
	login, _ := iamService.Login(t.Context(), user.Email, "password123")
	workspaces, _ := iamService.ListUserWorkspaces(t.Context(), user.ID)
	workspaceID := workspaces[0].ID
	agents := agent.NewService()
	for index, name := range []string{"writer", "reviewer"} {
		item, createErr := agents.CreateAgent(t.Context(), workspaceID, name, "blank")
		if createErr != nil {
			t.Fatal(createErr)
		}
		versionID := name + "-v1"
		version := domain.AgentVersion{ID: versionID, AgentID: item.ID, WorkspaceID: workspaceID, Version: 1, CreatedAt: time.Now().UTC()}
		snapshot := engine.AgentSnapshot{ID: versionID, AgentID: item.ID, WorkspaceID: workspaceID, SystemPrompt: name, MaxSteps: index + 2}
		if err := agents.Publish(t.Context(), version, snapshot); err != nil {
			t.Fatal(err)
		}
		if err := agents.Promote(t.Context(), workspaceID, item.ID, "dev", versionID); err != nil {
			t.Fatal(err)
		}
	}
	items := agents.ListAgents(t.Context(), workspaceID)
	teams := platformteam.NewService(agents)
	router := NewRouterWithControlPlane(iamService, completeChatRuntime{}, ControlPlane{Agents: agents, Teams: teams})
	body := fmt.Sprintf(`{"name":"content","mode":"pipeline","members":[{"agent_id":%q,"role":"write"},{"agent_id":%q,"role":"review"}]}`, items[0].ID, items[1].ID)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/teams", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", workspaceID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create team status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Team struct {
			ID string `json:"id"`
		} `json:"team"`
		Version platformteam.Version `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if len(created.Version.Members) != 2 || created.Version.Members[0].AgentVersionID == "" {
		t.Fatalf("version=%+v", created.Version)
	}
	runBody := fmt.Sprintf(`{"team_id":%q,"env":"dev","input":"draft"}`, created.Team.ID)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/teams/runs", strings.NewReader(runBody))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("X-Workspace-ID", workspaceID)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"final":"course reply"`) || !strings.Contains(response.Body.String(), `"steps"`) {
		t.Fatalf("run team status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminConsoleReadRouteInventory(t *testing.T) {
	t.Parallel()
	iamService := iam.New(iam.NewMemoryStore(), "0123456789abcdef0123456789abcdef", "kbot-test")
	user, err := iamService.Register(t.Context(), "console@example.com", "password123", "Console")
	if err != nil {
		t.Fatal(err)
	}
	login, _ := iamService.Login(t.Context(), user.Email, "password123")
	workspaces, _ := iamService.ListUserWorkspaces(t.Context(), user.ID)
	agents := agent.NewService()
	control := ControlPlane{
		Agents: agents, Approvals: approval.NewService(), Audit: audit.NewLedger(),
		Tools: platformtool.NewRegistry(), KBs: kb.NewService(), Prompts: prompt.NewService(),
		Profiles: modelconfig.NewRegistry(), Skills: skill.NewService(),
		Guard: guard.NewService(nil), Evaluator: platformeval.NewService(), EvalData: platformeval.NewCatalog(),
		Teams: platformteam.NewService(agents),
	}
	router := NewRouterWithControlPlane(iamService, completeChatRuntime{}, control)
	paths := []string{
		"/api/v1/users", "/api/v1/workspaces", "/api/v1/agents", "/api/v1/conversations",
		"/api/v1/tools", "/api/v1/kbs", "/api/v1/prompts", "/api/v1/model-accounts",
		"/api/v1/model-deployments", "/api/v1/model-profiles", "/api/v1/model-profile-versions",
		"/api/v1/skills", "/api/v1/teams", "/api/v1/guard/rules", "/api/v1/guard/quotas",
		"/api/v1/guard/injection-logs", "/api/v1/audit/logs", "/api/v1/eval/datasets",
		"/api/v1/approvals", "/api/v1/observability",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+login.Token)
		request.Header.Set("X-Workspace-ID", workspaces[0].ID)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
