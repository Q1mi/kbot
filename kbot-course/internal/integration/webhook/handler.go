// Package webhook 验证并解析通用 Webhook。
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"

	"github.com/Q1mi/kbot/internal/integration/replay"
)

const maxBody = 1 << 20

func Sign(secret string, body []byte) string {
	return SignAt(secret, "", "", body)
}

// SignAt binds the timestamp and nonce into the MAC, preventing a valid body
// from being replayed with fresh headers.
func SignAt(secret, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func NewHandler(secret string, consume func([]byte) error) http.Handler {
	return NewHandlerWithReplay(secret, replay.New(replay.DefaultWindow), consume)
}

func NewHandlerWithReplay(secret string, replayCache replay.Guard, consume func([]byte) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if secret == "" || consume == nil {
			http.Error(w, "webhook unavailable", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil || len(body) > maxBody {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		timestamp, nonce := r.Header.Get("X-Webhook-Timestamp"), r.Header.Get("X-Webhook-Nonce")
		expected := SignAt(secret, timestamp, nonce, body)
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Signature")), []byte(expected)) != 1 {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if replayCache == nil {
			http.Error(w, "replay protection unavailable", http.StatusServiceUnavailable)
			return
		}
		duplicate, err := replayCache.Accept(timestamp, "webhook:"+nonce+":"+expected)
		if err != nil {
			http.Error(w, "stale webhook", http.StatusUnauthorized)
			return
		}
		if duplicate {
			http.Error(w, "duplicate webhook", http.StatusConflict)
			return
		}
		if err := consume(body); err != nil {
			http.Error(w, fmt.Sprintf("consume event: %v", err), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}
