// Package streaming 提供流式API实现
package streaming

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/go-chi/chi/v5"
)

// SSEHandler SSE流式处理器
type SSEHandler struct {
	runtime *engine.Engine
}

// NewSSEHandler 创建SSE处理器
func NewSSEHandler(runtime *engine.Engine) *SSEHandler {
	return &SSEHandler{runtime: runtime}
}

// ChatRequest 流式聊天请求
type ChatRequest struct {
	Message             string         `json:"message"`
	ConversationID      string         `json:"conversation_id,omitempty"`
	AgentEnv            string         `json:"agent_env,omitempty"`
	UserPromptVersionID string         `json:"user_prompt_version_id,omitempty"`
	UserPromptVariables map[string]any `json:"user_prompt_variables,omitempty"`
}

// ChatStream SSE流式聊天
func (h *SSEHandler) ChatStream(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	userID := middleware.GetUserIDFromContext(r.Context())

	var req ChatRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 设置SSE响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 创建流式请求
	streamReq := engine.ChatStreamRequest{
		AgentID:             agentID,
		WorkspaceID:         middleware.GetWorkspaceIDFromContext(r.Context()),
		AgentEnv:            req.AgentEnv,
		ConversationID:      req.ConversationID,
		Message:             req.Message,
		UserID:              userID,
		UserPromptVersionID: req.UserPromptVersionID,
		UserPromptVariables: req.UserPromptVariables,
	}

	eventCh, err := h.runtime.ChatStream(r.Context(), streamReq)
	if err != nil {
		_ = writeSSEEvent(w, engine.EventError, engine.AgentEvent{Type: engine.EventError, Text: err.Error()})
		return
	}

	// 流式发送事件
	for event := range eventCh {
		if err := writeSSEEvent(w, event.Type, event); err != nil {
			return
		}

		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}

		// 检查客户端是否断开连接
		if r.Context().Err() != nil {
			return
		}
	}
}

// writeSSEEvent 写入SSE事件
func writeSSEEvent(w http.ResponseWriter, eventType string, data interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(dataJSON))
	return err
}
