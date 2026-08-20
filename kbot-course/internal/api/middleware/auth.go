package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Q1mi/kbot/internal/platform/iam"
)

type userIDKey struct{}
type workspaceIDKey struct{}
type workspaceRoleKey struct{}

func Auth(service *iam.Service) Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if raw == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			userID, err := service.ParseToken(raw)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Workspace(service *iam.Service) Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID := strings.TrimSpace(r.Header.Get("X-Workspace-ID"))
			if workspaceID == "" {
				http.Error(w, "workspace is required", http.StatusBadRequest)
				return
			}
			if service == nil {
				http.Error(w, "workspace access denied", http.StatusForbidden)
				return
			}
			role, err := service.WorkspaceRole(r.Context(), UserID(r.Context()), workspaceID)
			if err != nil || !workspaceMethodAllowed(role, r.Method, r.URL.Path) {
				http.Error(w, "workspace access denied", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), workspaceIDKey{}, workspaceID)
			ctx = context.WithValue(ctx, workspaceRoleKey{}, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserID(ctx context.Context) string {
	value, _ := ctx.Value(userIDKey{}).(string)
	return value
}

func WorkspaceID(ctx context.Context) string {
	value, _ := ctx.Value(workspaceIDKey{}).(string)
	return value
}

func WorkspaceRole(ctx context.Context) string {
	value, _ := ctx.Value(workspaceRoleKey{}).(string)
	return value
}

// workspaceMethodAllowed 保留清晰的课堂规则：viewer 只读，member 可运行已发布能力，
// editor 可修改控制面，审批动作只允许 owner/admin。
func workspaceMethodAllowed(role, method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead && strings.Contains(path, "/approvals/") {
		return role == iam.WorkspaceRoleOwner || role == iam.WorkspaceRoleAdmin
	}
	if role == iam.WorkspaceRoleOwner || role == iam.WorkspaceRoleAdmin || role == iam.WorkspaceRoleEditor {
		return true
	}
	if method == http.MethodGet || method == http.MethodHead {
		return true
	}
	if role == iam.WorkspaceRoleViewer {
		return false
	}
	return strings.HasSuffix(path, "/chat") ||
		strings.HasSuffix(path, "/search") ||
		strings.HasSuffix(path, "/runs") ||
		strings.Contains(path, "/stream/")
}
