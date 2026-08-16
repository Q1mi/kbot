package streaming

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// WSHandler WebSocket处理器
type WSHandler struct {
	runtime        *engine.Engine
	originPatterns []string
}

// NewWSHandler 创建WebSocket处理器
func NewWSHandler(runtime *engine.Engine, originPatterns []string) *WSHandler {
	return &WSHandler{runtime: runtime, originPatterns: append([]string(nil), originPatterns...)}
}

// WSMessage WebSocket消息
type WSMessage struct {
	Type                string         `json:"type"`
	Message             string         `json:"message,omitempty"`
	ConversationID      string         `json:"conversation_id,omitempty"`
	AgentEnv            string         `json:"agent_env,omitempty"`
	UserPromptVersionID string         `json:"user_prompt_version_id,omitempty"`
	UserPromptVariables map[string]any `json:"user_prompt_variables,omitempty"`
}

// ChatWebSocket WebSocket聊天
func (h *WSHandler) ChatWebSocket(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns,
		Subprotocols:   []string{"kbot.v1"},
	})
	if err != nil {
		log.Printf("websocket accept failed: %v", err)
		return
	}
	defer conn.CloseNow()

	// 处理WebSocket连接
	h.handleWSConnection(r.Context(), conn, agentID, workspaceID, userID)
}

// handleWSConnection 处理WebSocket连接
func (h *WSHandler) handleWSConnection(ctx context.Context, conn *websocket.Conn, agentID, workspaceID, userID string) {
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		// 读取客户端消息
		var msg WSMessage
		_, data, err := conn.Read(ctx)
		if err != nil {
			log.Printf("websocket read failed: %v", err)
			return
		}

		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("websocket unmarshal failed: %v", err)
			continue
		}

		switch msg.Type {
		case "chat":
			h.handleChatMessage(ctx, conn, agentID, workspaceID, userID, msg)
		case "ping":
			// 发送pong响应
			pong := map[string]string{"type": "pong"}
			h.writeJSON(ctx, conn, pong)
		default:
			log.Printf("unknown message type: %s", msg.Type)
		}
	}
}

// handleChatMessage 处理聊天消息
func (h *WSHandler) handleChatMessage(ctx context.Context, conn *websocket.Conn, agentID, workspaceID, userID string, msg WSMessage) {
	// 创建流式请求
	streamReq := engine.ChatStreamRequest{
		AgentID:             agentID,
		WorkspaceID:         workspaceID,
		AgentEnv:            msg.AgentEnv,
		ConversationID:      msg.ConversationID,
		Message:             msg.Message,
		UserID:              userID,
		UserPromptVersionID: msg.UserPromptVersionID,
		UserPromptVariables: msg.UserPromptVariables,
	}

	eventCh, err := h.runtime.ChatStream(ctx, streamReq)
	if err != nil {
		errorMsg := map[string]interface{}{
			"type":    "error",
			"message": err.Error(),
		}
		h.writeJSON(ctx, conn, errorMsg)
		return
	}

	// 流式发送事件
	for event := range eventCh {
		if err := h.writeJSON(ctx, conn, event); err != nil {
			log.Printf("websocket write failed: %v", err)
			return
		}

		// 检查上下文是否被取消
		if ctx.Err() != nil {
			return
		}
	}
}

// writeJSON 写入JSON数据到WebSocket
func (h *WSHandler) writeJSON(ctx context.Context, conn *websocket.Conn, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, jsonData)
}
