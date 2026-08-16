// Package lark 是飞书 bot 的 reference 入站适配器（设计文档 §10 / 讲义 §15.8）。
//
// 课堂重点放在“先抽象再实现”的工程理由。这里实现入站签名校验与事件
// 归一化（飞书事件订阅 v2 的 schema）。出站消息由同包的 Outbound
// 通过 larksuite/oapi-sdk-go 发送。
package lark

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Q1mi/kbot/internal/integration"
)

// Adapter 飞书入站适配器。
type Adapter struct {
	verifyToken string // 事件订阅 Verification Token（位于 JSON body）
	encryptKey  string // 事件订阅 Encrypt Key（用于 X-Lark-Signature）
}

// New 创建飞书适配器。
func New(verifyToken string, encryptKey ...string) *Adapter {
	a := &Adapter{verifyToken: verifyToken}
	if len(encryptKey) > 0 {
		a.encryptKey = encryptKey[0]
	}
	return a
}

// Verify 校验飞书事件订阅 token，并在配置 Encrypt Key 时校验签名：
// sha256(timestamp + nonce + encrypt_key + body)。
// 头：X-Lark-Request-Timestamp / X-Lark-Request-Nonce / X-Lark-Signature。
func (a *Adapter) Verify(headers http.Header, body []byte) error {
	if a.verifyToken == "" && a.encryptKey == "" {
		return fmt.Errorf("lark: verification is not configured")
	}
	if a.encryptKey != "" {
		ts := headers.Get("X-Lark-Request-Timestamp")
		nonce := headers.Get("X-Lark-Request-Nonce")
		sig := headers.Get("X-Lark-Signature")
		if ts == "" || nonce == "" || sig == "" {
			return fmt.Errorf("lark: missing signature")
		}
		expected := Sign(ts, nonce, a.encryptKey, body)
		if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
			return fmt.Errorf("lark: signature mismatch")
		}
	}
	plain, err := a.decryptEnvelope(body)
	if err != nil {
		return err
	}
	if a.verifyToken != "" {
		var envelope struct {
			Token  string `json:"token"`
			Header struct {
				Token string `json:"token"`
			} `json:"header"`
		}
		if err := json.Unmarshal(plain, &envelope); err != nil {
			return fmt.Errorf("lark: parse verification token: %w", err)
		}
		token := envelope.Header.Token
		if token == "" {
			token = envelope.Token
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.verifyToken)) != 1 {
			return fmt.Errorf("lark: verification token mismatch")
		}
	}
	return nil
}

// Sign 计算飞书事件签名（导出便于测试与对端构造）。
func Sign(timestamp, nonce, token string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(timestamp + nonce + token))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// larkEvent 是飞书事件订阅的简化结构。
type larkEvent struct {
	// URL 校验时的字段。
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
	// 事件 v2。
	Header struct {
		EventType string `json:"event_type"`
		EventID   string `json:"event_id"`
		Token     string `json:"token"`
	} `json:"header"`
	Event struct {
		Sender struct {
			SenderID struct {
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
		} `json:"sender"`
		Message struct {
			ChatID  string `json:"chat_id"`
			Content string `json:"content"` // JSON 字符串，如 {"text":"hello"}
		} `json:"message"`
	} `json:"event"`
}

// Translate 把飞书事件归一化成 Inbound。URL 校验请求只回显 challenge。
func (a *Adapter) Translate(body []byte) (*integration.Inbound, error) {
	plain, err := a.decryptEnvelope(body)
	if err != nil {
		return nil, err
	}
	var e larkEvent
	if err := json.Unmarshal(plain, &e); err != nil {
		return nil, fmt.Errorf("lark: parse event: %w", err)
	}
	if e.Type == "url_verification" {
		if strings.TrimSpace(e.Challenge) == "" {
			return nil, fmt.Errorf("lark: empty challenge")
		}
		return &integration.Inbound{Challenge: e.Challenge}, nil
	}
	if e.Header.EventType != "im.message.receive_v1" {
		return nil, fmt.Errorf("lark: unsupported event type %q", e.Header.EventType)
	}

	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(e.Event.Message.Content), &content); err != nil {
		return nil, fmt.Errorf("lark: parse message content: %w", err)
	}
	if strings.TrimSpace(e.Event.Message.ChatID) == "" || strings.TrimSpace(content.Text) == "" {
		return nil, fmt.Errorf("lark: chat_id and text are required")
	}

	return &integration.Inbound{
		EventID: e.Header.EventID,
		Channel: e.Event.Message.ChatID,
		UserID:  e.Event.Sender.SenderID.OpenID,
		Text:    content.Text,
	}, nil
}

// decryptEnvelope 解开配置 Encrypt Key 后飞书发送的 {"encrypt":"base64"} 载荷。
func (a *Adapter) decryptEnvelope(body []byte) ([]byte, error) {
	var envelope struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("lark: parse event envelope: %w", err)
	}
	if envelope.Encrypt == "" {
		return body, nil
	}
	if a.encryptKey == "" {
		return nil, fmt.Errorf("lark: encrypted event requires encrypt key")
	}
	encoded, err := base64.StdEncoding.DecodeString(envelope.Encrypt)
	if err != nil {
		return nil, fmt.Errorf("lark: decode encrypted event: %w", err)
	}
	if len(encoded) <= aes.BlockSize || (len(encoded)-aes.BlockSize)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("lark: invalid encrypted event length")
	}
	key := sha256.Sum256([]byte(a.encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("lark: initialize cipher: %w", err)
	}
	iv, ciphertext := encoded[:aes.BlockSize], encoded[aes.BlockSize:]
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plain) {
		return nil, fmt.Errorf("lark: invalid encrypted event padding")
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("lark: invalid encrypted event padding")
		}
	}
	return plain[:len(plain)-padding], nil
}
