package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type stubRuntime struct{}

func (stubRuntime) ChatStream(_ context.Context, _ engine.ChatRequest, emit engine.Emitter) error {
	return emit(engine.Event{Type: "answer_delta", Text: "你好"})
}

type reportedErrorRuntime struct{}

func (reportedErrorRuntime) ChatStream(_ context.Context, _ engine.ChatRequest, emit engine.Emitter) error {
	if err := emit(engine.Event{Type: "error", Data: map[string]string{"message": "boom"}}); err != nil {
		return err
	}
	return errors.New("boom")
}

func TestStreamHandlerWritesSSEFrame(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/stream/agents/demo/chat", strings.NewReader(`{"conversation_id":"c1","message":"hi"}`))
	recorder := httptest.NewRecorder()
	NewStreamHandler(stubRuntime{}).ServeHTTP(recorder, req)
	if recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(recorder.Body.String(), `"type":"answer_delta"`) || !strings.Contains(recorder.Body.String(), "\n\n") {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestStreamHandlerDoesNotDuplicateRuntimeErrorEvent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/stream/agents/demo/chat", strings.NewReader(`{"conversation_id":"c1","message":"hi"}`))
	recorder := httptest.NewRecorder()
	NewStreamHandler(reportedErrorRuntime{}).ServeHTTP(recorder, req)
	if count := strings.Count(recorder.Body.String(), `"type":"error"`); count != 1 {
		t.Fatalf("error event count = %d, body = %q", count, recorder.Body.String())
	}
}
