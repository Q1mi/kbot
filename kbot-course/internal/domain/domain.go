// Package domain 定义控制面与运行时共享的核心领域对象。
package domain

import "time"

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
	AgentID        string
	AgentVersionID string
	UserID         string
	CreatedAt      time.Time
}
