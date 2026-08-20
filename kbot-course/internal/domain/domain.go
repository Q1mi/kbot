// Package domain 定义控制面与运行时共享的核心领域对象。
package domain

import "time"

// User 是平台登录身份。
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
}

// Workspace 是业务资源的逻辑隔离边界。
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Membership 将登录身份绑定到允许访问的 Workspace。
// Workspace header 只负责选择范围，访问权始终从服务端成员关系解析。
type Membership struct {
	UserID      string    `json:"user_id"`
	WorkspaceID string    `json:"workspace_id"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// AgentVersion 是 Agent 的不可变版本。
// 新配置通过创建新版本发布，已有版本不再原地修改。
type AgentVersion struct {
	ID           string
	AgentID      string
	WorkspaceID  string
	Version      int
	SystemPrompt string
	CreatedAt    time.Time
}

// Conversation 固定一次会话实际使用的 AgentVersion。
// 后续发布新版本不会改变已经创建的会话。
type Conversation struct {
	ID             string
	WorkspaceID    string
	AgentID        string
	AgentVersionID string
	UserID         string
	CreatedAt      time.Time
}

// Message 是会话中的持久化消息。
type Message struct {
	ID             string
	ConversationID string
	Role           string
	Content        string
	CreatedAt      time.Time
}
