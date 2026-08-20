package api

import (
	"context"
	"net/http"

	"github.com/Q1mi/kbot/internal/runtime/engine"
)

// ChatRuntime 让 HTTP 层只感知事件流，不依赖 Engine 的内部步骤。
type ChatRuntime interface {
	ChatStream(ctx context.Context, req engine.ChatRequest, emit engine.Emitter) error
}

// StreamHandler 的 SSE 编码与取消传播会在 06-end 完成。
type StreamHandler struct{}

func NewStreamHandler() *StreamHandler { return &StreamHandler{} }

func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "implement in 06-end", http.StatusNotImplemented)
}
