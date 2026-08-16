// Package webhook 是通用 Webhook 触发器（设计文档 §10 / 讲义 §15.8）：
// 外部系统调 webhook → 触发一个 Agent → 结果回写。
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Q1mi/kbot/internal/integration"
)

// ErrNotConfigured 表示 Webhook 未配置签名密钥。
var ErrNotConfigured = errors.New("webhook not configured")

// Adapter 通用 webhook 入站适配器，用 HMAC-SHA256 校验签名。
type Adapter struct {
	secret string
}

// New 创建 webhook 适配器。
func New(secret string) *Adapter {
	return &Adapter{secret: secret}
}

// Verify 校验时间窗和 X-Signature = HMAC(secret, timestamp + "." + nonce + "." + body)。
func (a *Adapter) Verify(headers http.Header, body []byte) error {
	if a.secret == "" {
		return ErrNotConfigured
	}
	sig := headers.Get("X-Signature")
	timestamp := headers.Get("X-Kbot-Timestamp")
	nonce := headers.Get("X-Kbot-Nonce")
	if sig == "" || timestamp == "" || nonce == "" {
		return fmt.Errorf("webhook: signature, timestamp and nonce are required")
	}
	unixSeconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(unixSeconds, 0)) > 5*time.Minute || time.Until(time.Unix(unixSeconds, 0)) > 5*time.Minute {
		return fmt.Errorf("webhook: timestamp is outside the allowed window")
	}
	expected := SignHMAC(a.secret, timestamp, nonce, body)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return fmt.Errorf("webhook: signature mismatch")
	}
	return nil
}

// SignHMAC 计算 HMAC-SHA256 签名（导出便于测试与调用方构造）。
func SignHMAC(secret, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// payload 是 webhook 触发的载荷：指定 agent + 文本输入。
type payload struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Input       string `json:"input"`
	UserID      string `json:"user_id"`
}

// Translate 把 webhook payload 归一化成 Inbound（Channel 用 agent_id 承载）。
func (a *Adapter) Translate(body []byte) (*integration.Inbound, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("webhook: parse payload: %w", err)
	}
	if p.WorkspaceID == "" || p.AgentID == "" || p.Input == "" {
		return nil, fmt.Errorf("webhook: workspace_id, agent_id and input are required")
	}
	return &integration.Inbound{WorkspaceID: p.WorkspaceID, Channel: p.AgentID, UserID: p.UserID, Text: p.Input}, nil
}
