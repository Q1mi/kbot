package middleware

import (
	"net/http"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/iam"
)

func TestWorkspaceMethodAllowed(t *testing.T) {
	tests := []struct {
		name, role, method, path string
		want                     bool
	}{
		{"viewer reads", iam.WorkspaceRoleViewer, http.MethodGet, "/api/v1/tools", true},
		{"viewer cannot publish", iam.WorkspaceRoleViewer, http.MethodPost, "/api/v1/tools", false},
		{"member chats", iam.WorkspaceRoleMember, http.MethodPost, "/api/v1/agents/a1/chat", true},
		{"member cannot publish", iam.WorkspaceRoleMember, http.MethodPost, "/api/v1/tools", false},
		{"editor publishes", iam.WorkspaceRoleEditor, http.MethodPost, "/api/v1/tools", true},
		{"editor cannot approve", iam.WorkspaceRoleEditor, http.MethodPost, "/api/v1/approvals/p1/approve", false},
		{"admin approves", iam.WorkspaceRoleAdmin, http.MethodPost, "/api/v1/approvals/p1/approve", true},
		{"owner approves", iam.WorkspaceRoleOwner, http.MethodPost, "/api/v1/approvals/p1/approve", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := workspaceMethodAllowed(test.role, test.method, test.path); got != test.want {
				t.Fatalf("workspaceMethodAllowed() = %v, want %v", got, test.want)
			}
		})
	}
}
