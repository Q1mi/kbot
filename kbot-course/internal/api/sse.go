package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type ChatRuntime interface {
	ChatStream(ctx context.Context, req engine.ChatRequest, emit engine.Emitter) error
}

type StreamHandler struct{ runtime ChatRuntime }

func NewStreamHandler(runtime ChatRuntime) *StreamHandler {
	return &StreamHandler{runtime: runtime}
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	errorEventWritten := false
	err := h.runtime.ChatStream(r.Context(), req, func(event engine.Event) error {
		if event.Type == "error" {
			errorEventWritten = true
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
	if err != nil && !errorsIsCanceled(err) && !errorEventWritten {
		payload, _ := json.Marshal(engine.Event{Type: "error", Data: map[string]string{"message": err.Error()}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
}

func errorsIsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
