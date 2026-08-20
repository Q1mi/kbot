package lark

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandlerVerifiesLarkSignature(t *testing.T) {
	body := []byte(`{"header":{"event_id":"event-1"}}`)
	handler := NewHandler("encrypt-key", func([]byte) error { return nil })
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/lark", strings.NewReader(string(body)))
	req.Header.Set("X-Lark-Request-Timestamp", timestamp)
	req.Header.Set("X-Lark-Request-Nonce", "nonce")
	req.Header.Set("X-Lark-Signature", Sign(timestamp, "nonce", "encrypt-key", body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	duplicate := httptest.NewRequest(http.MethodPost, "/lark", strings.NewReader(string(body)))
	duplicate.Header = req.Header.Clone()
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, duplicate)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
