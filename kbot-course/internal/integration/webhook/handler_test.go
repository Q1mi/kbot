package webhook

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandlerVerifiesHMACBeforeConsuming(t *testing.T) {
	body := []byte(`{"event_id":"event-1"}`)
	consumed := false
	handler := NewHandler("secret", func(got []byte) error { consumed = string(got) == string(body); return nil })
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Webhook-Timestamp", timestamp)
	req.Header.Set("X-Webhook-Nonce", "nonce-1")
	req.Header.Set("X-Signature", SignAt("secret", timestamp, "nonce-1", body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted || !consumed {
		t.Fatalf("status=%d consumed=%v", recorder.Code, consumed)
	}
	duplicate := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	duplicate.Header = req.Header.Clone()
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, duplicate)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d", recorder.Code)
	}
	bad := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	bad.Header.Set("X-Webhook-Timestamp", timestamp)
	bad.Header.Set("X-Webhook-Nonce", "nonce-2")
	bad.Header.Set("X-Signature", "bad")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, bad)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("bad status=%d", recorder.Code)
	}
}
