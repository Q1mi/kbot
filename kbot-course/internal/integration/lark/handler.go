// Package lark 将飞书事件转换为统一渠道消息。
package lark

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/Q1mi/kbot/internal/integration/replay"
)

const maxBody = 1 << 20

// Sign 遵循飞书事件订阅签名：sha256(timestamp + nonce + encryptKey + body)。
func Sign(timestamp, nonce, encryptKey string, body []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(timestamp + nonce + encryptKey))
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

func NewHandler(encryptKey string, consume func([]byte) error) http.Handler {
	return NewHandlerWithReplay(encryptKey, replay.New(replay.DefaultWindow), consume)
}

func NewHandlerWithReplay(encryptKey string, replayCache replay.Guard, consume func([]byte) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if encryptKey == "" || consume == nil {
			http.Error(w, "lark integration unavailable", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil || len(body) > maxBody {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		timestamp, nonce := r.Header.Get("X-Lark-Request-Timestamp"), r.Header.Get("X-Lark-Request-Nonce")
		expected := Sign(timestamp, nonce, encryptKey, body)
		if timestamp == "" || nonce == "" || encryptKey == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Lark-Signature")), []byte(expected)) != 1 {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if replayCache == nil {
			http.Error(w, "replay protection unavailable", http.StatusServiceUnavailable)
			return
		}
		duplicate, err := replayCache.Accept(timestamp, "lark:"+nonce+":"+expected)
		if err != nil {
			http.Error(w, "stale event", http.StatusUnauthorized)
			return
		}
		if duplicate {
			http.Error(w, "duplicate event", http.StatusConflict)
			return
		}
		if consume(body) != nil {
			http.Error(w, "consume event failed", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}
