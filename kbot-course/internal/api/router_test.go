package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/iam"
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
