package webhook

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestVerifyFailsClosedWithoutSecret(t *testing.T) {
	if err := New("").Verify(http.Header{}, []byte(`{}`)); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Verify() error = %v, want ErrNotConfigured", err)
	}
}

func TestVerifyAndTranslate(t *testing.T) {
	a := New("topsecret")
	body := []byte(`{"workspace_id":"w1","agent_id":"a1","input":"诊断告警","user_id":"monitor"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "event-1"
	sig := SignHMAC("topsecret", timestamp, nonce, body)

	h := http.Header{}
	h.Set("X-Signature", sig)
	h.Set("X-Kbot-Timestamp", timestamp)
	h.Set("X-Kbot-Nonce", nonce)
	if err := a.Verify(h, body); err != nil {
		t.Fatalf("expected valid hmac, got %v", err)
	}

	in, err := a.Translate(body)
	if err != nil {
		t.Fatal(err)
	}
	if in.WorkspaceID != "w1" || in.Channel != "a1" || in.Text != "诊断告警" {
		t.Fatalf("unexpected inbound: %+v", in)
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	a := New("topsecret")
	h := http.Header{}
	h.Set("X-Signature", "deadbeef")
	h.Set("X-Kbot-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	h.Set("X-Kbot-Nonce", "event-2")
	if err := a.Verify(h, []byte(`{}`)); err == nil {
		t.Fatal("expected signature mismatch")
	}
}

func TestTranslateRequiresFields(t *testing.T) {
	a := New("")
	if _, err := a.Translate([]byte(`{"agent_id":"a1"}`)); err == nil {
		t.Fatal("expected error for missing input")
	}
}
