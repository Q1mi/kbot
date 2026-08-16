package lark

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	a := New("verify-token", "encrypt-key")
	body := []byte(`{"header":{"token":"verify-token"},"hello":"world"}`)
	ts, nonce := "1700000000", "abc123"
	sig := Sign(ts, nonce, "encrypt-key", body)

	h := http.Header{}
	h.Set("X-Lark-Request-Timestamp", ts)
	h.Set("X-Lark-Request-Nonce", nonce)
	h.Set("X-Lark-Signature", sig)
	if err := a.Verify(h, body); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}

	// 篡改 body → 校验失败。
	if err := a.Verify(h, []byte(`{"hello":"tampered"}`)); err == nil {
		t.Fatal("expected signature mismatch for tampered body")
	}
}

func TestEncryptedURLVerification(t *testing.T) {
	const encryptKey = "classroom-encrypt-key"
	plain := []byte(`{"type":"url_verification","challenge":"encrypted-challenge","token":"verify-token"}`)
	block, err := aes.NewCipher(func() []byte { sum := sha256.Sum256([]byte(encryptKey)); return sum[:] }())
	if err != nil {
		t.Fatal(err)
	}
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte(nil), plain...), make([]byte, padding)...)
	for i := len(padded) - padding; i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	iv := []byte("0123456789abcdef")
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	encrypted := append(append([]byte(nil), iv...), ciphertext...)
	body, err := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(encrypted)})
	if err != nil {
		t.Fatal(err)
	}
	ts, nonce := "1700000000", "encrypted-nonce"
	headers := http.Header{}
	headers.Set("X-Lark-Request-Timestamp", ts)
	headers.Set("X-Lark-Request-Nonce", nonce)
	headers.Set("X-Lark-Signature", Sign(ts, nonce, encryptKey, body))
	adapter := New("verify-token", encryptKey)
	if err := adapter.Verify(headers, body); err != nil {
		t.Fatal(err)
	}
	inbound, err := adapter.Translate(body)
	if err != nil || inbound.Challenge != "encrypted-challenge" {
		t.Fatalf("encrypted challenge mismatch: %+v err=%v", inbound, err)
	}
}

func TestTranslateURLVerification(t *testing.T) {
	a := New("verify-token")
	raw := []byte(`{"type":"url_verification","challenge":"ch-123","token":"verify-token"}`)
	if err := a.Verify(http.Header{}, raw); err != nil {
		t.Fatal(err)
	}
	in, err := a.Translate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.Challenge != "ch-123" {
		t.Fatalf("expected challenge echo, got %q", in.Challenge)
	}
}

func TestTranslateMessageEvent(t *testing.T) {
	a := New("verify-token")
	raw := `{
		"header": {"event_type": "im.message.receive_v1", "event_id":"evt-1", "token":"verify-token"},
		"event": {
			"sender": {"sender_id": {"open_id": "ou_123"}},
			"message": {"chat_id": "oc_abc", "content": "{\"text\":\"你好 kbot\"}"}
		}
	}`
	in, err := a.Translate([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if in.Channel != "oc_abc" || in.UserID != "ou_123" || in.Text != "你好 kbot" {
		t.Fatalf("unexpected inbound: %+v", in)
	}
	if in.EventID != "evt-1" {
		t.Fatalf("unexpected event id: %q", in.EventID)
	}
}

func TestVerifyFailsClosedWithoutConfiguration(t *testing.T) {
	if err := New("").Verify(http.Header{}, []byte(`{}`)); err == nil {
		t.Fatal("expected unconfigured verification to fail")
	}
}
