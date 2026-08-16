package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	webhookintegration "github.com/Q1mi/kbot/internal/integration/webhook"
	"github.com/Q1mi/kbot/internal/runtime/cache"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type fakeIntegrationRunner struct{ calls chan engine.ChatStreamRequest }

func (f *fakeIntegrationRunner) Chat(_ context.Context, req engine.ChatStreamRequest) (string, error) {
	f.calls <- req
	return "飞书回复", nil
}

type retryIntegrationRunner struct {
	calls int
	err   error
}

func (f *retryIntegrationRunner) Chat(_ context.Context, _ engine.ChatStreamRequest) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return "cached reply", nil
}

type fakeLarkQueue struct {
	calls chan jobs.LarkReplyPayload
	err   error
}

func (f *fakeLarkQueue) EnqueueLarkReply(_ context.Context, payload jobs.LarkReplyPayload) error {
	if f.err != nil {
		return f.err
	}
	f.calls <- payload
	return nil
}

func TestLarkEventsChallenge(t *testing.T) {
	h := &IntegrationHandler{runtime: &fakeIntegrationRunner{}, replay: cache.NewMemoryIdemStore()}
	h.WithLark("verify-token", "", "agent-1", true)
	req := httptest.NewRequest(http.MethodPost, "/integrations/lark/events",
		strings.NewReader(`{"type":"url_verification","challenge":"challenge-1","token":"verify-token"}`))
	rec := httptest.NewRecorder()
	h.LarkEvents(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "challenge-1") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLarkEventsEnqueuesDurableReply(t *testing.T) {
	queue := &fakeLarkQueue{calls: make(chan jobs.LarkReplyPayload, 1)}
	h := &IntegrationHandler{replay: cache.NewMemoryIdemStore(), larkQueue: queue}
	h.WithLark("verify-token", "", "agent-1", true)
	body, _ := json.Marshal(map[string]any{
		"header": map[string]any{"event_type": "im.message.receive_v1", "event_id": "evt-1", "token": "verify-token"},
		"event": map[string]any{
			"sender":  map[string]any{"sender_id": map[string]string{"open_id": "ou-1"}},
			"message": map[string]string{"chat_id": "oc-1", "content": `{"text":"你好"}`},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/integrations/lark/events", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	h.LarkEvents(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	call := <-queue.calls
	if call.EventID != "evt-1" || call.AgentID != "agent-1" || call.UserID != "ou-1" || call.Channel != "oc-1" || call.Text != "你好" {
		t.Fatalf("unexpected queued payload: %+v", call)
	}

	duplicate := httptest.NewRecorder()
	h.LarkEvents(duplicate, httptest.NewRequest(http.MethodPost, "/integrations/lark/events", strings.NewReader(string(body))))
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), "duplicate") {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

func TestLarkEventsQueueFailureCanBeRetried(t *testing.T) {
	queue := &fakeLarkQueue{calls: make(chan jobs.LarkReplyPayload, 1), err: errors.New("redis unavailable")}
	h := &IntegrationHandler{replay: cache.NewMemoryIdemStore(), larkQueue: queue}
	h.WithLark("verify-token", "", "agent-1", true)
	body := `{"header":{"event_type":"im.message.receive_v1","event_id":"evt-retry","token":"verify-token"},"event":{"sender":{"sender_id":{"open_id":"ou-1"}},"message":{"chat_id":"oc-1","content":"{\"text\":\"retry\"}"}}}`
	first := httptest.NewRecorder()
	h.LarkEvents(first, httptest.NewRequest(http.MethodPost, "/integrations/lark/events", strings.NewReader(body)))
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	queue.err = nil
	second := httptest.NewRecorder()
	h.LarkEvents(second, httptest.NewRequest(http.MethodPost, "/integrations/lark/events", strings.NewReader(body)))
	if second.Code != http.StatusAccepted {
		t.Fatalf("retry status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestLarkEventsRejectsMessagesWithoutOutboundCredentials(t *testing.T) {
	queue := &fakeLarkQueue{calls: make(chan jobs.LarkReplyPayload, 1)}
	h := &IntegrationHandler{replay: cache.NewMemoryIdemStore(), larkQueue: queue}
	h.WithLark("verify-token", "", "agent-1", false)
	body := `{"header":{"event_type":"im.message.receive_v1","event_id":"evt-no-outbound","token":"verify-token"},"event":{"sender":{"sender_id":{"open_id":"ou-1"}},"message":{"chat_id":"oc-1","content":"{\"text\":\"hello\"}"}}}`
	recorder := httptest.NewRecorder()
	h.LarkEvents(recorder, httptest.NewRequest(http.MethodPost, "/integrations/lark/events", strings.NewReader(body)))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestWebhookFailureReleasesNonceAndSuccessCachesReply(t *testing.T) {
	const secret = "webhook-secret-at-least-32-characters"
	runner := &retryIntegrationRunner{err: errors.New("model unavailable")}
	h := NewIntegrationHandler(secret, nil).WithReplayStore(cache.NewMemoryIdemStore())
	h.runtime = runner
	body := []byte(`{"workspace_id":"ws-1","agent_id":"agent-1","input":"hello","user_id":"external"}`)
	newRequest := func() *http.Request {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, "/integrations/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Kbot-Timestamp", timestamp)
		req.Header.Set("X-Kbot-Nonce", "webhook-retry-1")
		req.Header.Set("X-Signature", webhookintegration.SignHMAC(secret, timestamp, "webhook-retry-1", body))
		return req
	}
	first := httptest.NewRecorder()
	h.Webhook(first, newRequest())
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	runner.err = nil
	second := httptest.NewRecorder()
	h.Webhook(second, newRequest())
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "cached reply") {
		t.Fatalf("retry status=%d body=%s", second.Code, second.Body.String())
	}
	third := httptest.NewRecorder()
	h.Webhook(third, newRequest())
	if third.Code != http.StatusOK || third.Header().Get("X-Idempotent-Replay") != "true" || runner.calls != 2 {
		t.Fatalf("cached replay status=%d calls=%d headers=%v body=%s", third.Code, runner.calls, third.Header(), third.Body.String())
	}
}
