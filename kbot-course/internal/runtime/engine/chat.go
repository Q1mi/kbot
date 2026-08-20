package engine

import (
	"context"
	"errors"
)

var ErrChatStreamNotImplemented = errors.New("chat stream is implemented in 06-end")

// ChatRequest 是一次运行请求；ConversationID 用于多轮会话续接。
type ChatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
}

// Event 是 Runtime 对传输层公开的稳定事件信封。
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
	Text string `json:"text,omitempty"`
}

type Emitter func(Event) error

func (e *Engine) ChatStream(context.Context, ChatRequest, Emitter) error {
	return ErrChatStreamNotImplemented
}
