// Package integration 定义外部渠道接入后的统一消息。
package integration

type Message struct{ Source, EventID, WorkspaceID, UserID, ConversationID, Text string }
