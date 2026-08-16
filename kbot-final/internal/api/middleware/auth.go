// Package middleware 提供HTTP中间件
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Q1mi/kbot/internal/platform/iam"
)

// ContextKey 定义context key类型
type ContextKey string

const (
	ContextKeyUserID        ContextKey = "user_id"
	ContextKeyWorkspaceID   ContextKey = "workspace_id"
	ContextKeyGlobalRole    ContextKey = "global_role"
	ContextKeyWorkspaceRole ContextKey = "workspace_role"
)

// Auth JWT认证中间件
func Auth(iamService *iam.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" && strings.HasSuffix(r.URL.Path, "/ws") {
				if token := webSocketProtocolToken(r.Header.Get("Sec-WebSocket-Protocol")); token != "" {
					authHeader = "Bearer " + token
				}
			}
			if authHeader == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			// 提取Bearer token
			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				http.Error(w, "invalid authorization format", http.StatusUnauthorized)
				return
			}

			tokenString := authHeader[len(bearerPrefix):]
			claims, err := iamService.ValidateToken(tokenString)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			user, err := iamService.GetUser(r.Context(), claims.UserID)
			if err != nil || user.Status != "active" {
				http.Error(w, "user account is unavailable", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ContextKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ContextKeyGlobalRole, user.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// 浏览器 WebSocket API 无法设置 Authorization；客户端通过子协议
// ["kbot.v1", "kbot.jwt.<JWT>"] 传递同一枚短期访问令牌。
func webSocketProtocolToken(header string) string {
	for _, protocol := range strings.Split(header, ",") {
		protocol = strings.TrimSpace(protocol)
		if strings.HasPrefix(protocol, "kbot.jwt.") {
			return strings.TrimPrefix(protocol, "kbot.jwt.")
		}
	}
	return ""
}

// Workspace 工作空间中间件
func Workspace(iamService *iam.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 从header、query或path中提取workspace_id
			workspaceID := r.Header.Get("X-Workspace-ID")
			if workspaceID == "" {
				workspaceID = r.URL.Query().Get("workspace_id")
			}

			if workspaceID == "" {
				if workspaceOptionalPath(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "workspace id is required", http.StatusBadRequest)
				return
			}
			userID := GetUserIDFromContext(r.Context())
			role, err := iamService.WorkspaceRole(r.Context(), userID, workspaceID)
			if err != nil {
				http.Error(w, "workspace access denied", http.StatusForbidden)
				return
			}
			if !workspaceMethodAllowed(role, r.Method, r.URL.Path) {
				http.Error(w, "workspace role does not allow this action", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), ContextKeyWorkspaceID, workspaceID)
			ctx = context.WithValue(ctx, ContextKeyWorkspaceRole, role)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

func workspaceOptionalPath(path string) bool {
	switch path {
	case "/api/v1/auth/register", "/api/v1/users", "/api/v1/workspaces", "/api/v1/me", "/api/v1/observability":
		return true
	default:
		return strings.HasPrefix(path, "/api/v1/workspaces/")
	}
}

func workspaceMethodAllowed(role, method, path string) bool {
	if method != http.MethodGet && (strings.Contains(path, "/approvals/") || strings.HasSuffix(path, "/a2ui/actions")) {
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
	// member 可以运行已发布能力，但不能修改控制面配置。
	return strings.HasSuffix(path, "/chat") ||
		strings.HasSuffix(path, "/search") ||
		strings.HasSuffix(path, "/runs") ||
		strings.Contains(path, "/stream/")
}

// RequireGlobalAdmin 限制平台级用户管理接口。
func RequireGlobalAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if role, _ := r.Context().Value(ContextKeyGlobalRole).(string); role != iam.GlobalRoleAdmin {
				http.Error(w, "global admin role required", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetUserIDFromContext 从context获取用户ID
func GetUserIDFromContext(ctx context.Context) string {
	if userID, ok := ctx.Value(ContextKeyUserID).(string); ok {
		return userID
	}
	return ""
}

// GetWorkspaceIDFromContext 从context获取工作空间ID
func GetWorkspaceIDFromContext(ctx context.Context) string {
	if workspaceID, ok := ctx.Value(ContextKeyWorkspaceID).(string); ok {
		return workspaceID
	}
	return ""
}

func GetGlobalRoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(ContextKeyGlobalRole).(string)
	return role
}
