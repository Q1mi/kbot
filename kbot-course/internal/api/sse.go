package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type ChatRuntime interface {
	ChatStream(ctx context.Context, req engine.ChatRequest, emit engine.Emitter) error
}

type ConversationResolver interface {
	ResolveConversation(ctx context.Context, workspaceID, userID, agentID, environment, conversationID string) (*domain.Conversation, error)
}

type MessageRecorder interface {
	AppendMessage(ctx context.Context, workspaceID, conversationID, role, content string) error
}

type StreamHandler struct {
	runtime       ChatRuntime
	conversations ConversationResolver
	messages      MessageRecorder
}

func NewStreamHandler(runtime ChatRuntime) *StreamHandler {
	return &StreamHandler{runtime: runtime}
}

func (h *StreamHandler) WithConversations(conversations ConversationResolver) *StreamHandler {
	h.conversations = conversations
	if recorder, ok := conversations.(MessageRecorder); ok {
		h.messages = recorder
	}
	return h
}

func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		http.Error(w, "chat runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	var req engine.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.WorkspaceID = middleware.WorkspaceID(r.Context())
	req.UserID = middleware.UserID(r.Context())
	if h.conversations != nil {
		conversation, err := h.conversations.ResolveConversation(
			r.Context(), req.WorkspaceID, middleware.UserID(r.Context()), chi.URLParam(r, "agentID"),
			req.AgentEnvironment, req.ConversationID,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		req.ConversationID = conversation.ID
	} else if req.ConversationID == "" {
		http.Error(w, "conversation_id is required", http.StatusBadRequest)
		return
	}
	if h.messages != nil {
		if err := h.messages.AppendMessage(r.Context(), req.WorkspaceID, req.ConversationID, "user", req.Message); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	errorEventWritten := false
	answer := ""
	err := h.runtime.ChatStream(r.Context(), req, func(event engine.Event) error {
		if event.Type == "error" {
			errorEventWritten = true
		}
		if event.Type == "answer_done" {
			answer = event.Text
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if answer != "" && h.messages != nil {
		_ = h.messages.AppendMessage(r.Context(), req.WorkspaceID, req.ConversationID, "assistant", answer)
	}
	if err != nil && !errorsIsCanceled(err) && !errorEventWritten {
		payload, _ := json.Marshal(engine.Event{Type: "error", Data: map[string]string{"message": err.Error()}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
}

func errorsIsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
