package integration

import (
	"context"

	"github.com/Q1mi/kbot/internal/runtime/engine"
)

// ChatRuntime 是第 22 课渠道适配器调用统一 Runtime 的边界。
type ChatRuntime interface {
	ChatStream(context.Context, engine.ChatRequest, engine.Emitter) error
}
