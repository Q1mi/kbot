// Package integration 定义 IM 和 Webhook 的归一化入站消息。
package integration

// Inbound 是归一化后的入站消息（各家 IM 结构不同，translate 后统一成它）。
type Inbound struct {
	EventID     string // 上游事件 ID（用于幂等去重）
	Channel     string // 会话/群标识
	UserID      string // 发送者
	Text        string // 文本内容
	Challenge   string // URL 校验挑战（非空时只需回显，不进 Agent）
	WorkspaceID string // 目标 Agent 所属工作空间（Webhook 等外部入口必须显式给出）
}
