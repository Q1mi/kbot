package integration

import "testing"

func TestDecodeWebhookAndLarkMessages(t *testing.T) {
	webhook, err := decodeMessage("webhook", []byte(`{"event_id":"evt-1","user_id":"user-1","text":"hello"}`))
	if err != nil || webhook.EventID != "evt-1" || webhook.Text != "hello" {
		t.Fatalf("webhook = %+v, err = %v", webhook, err)
	}
	lark, err := decodeMessage("lark", []byte(`{"header":{"event_id":"evt-2"},"event":{"sender":{"sender_id":{"open_id":"ou-1"}},"message":{"content":"{\"text\":\"你好\"}"}}}`))
	if err != nil || lark.UserID != "ou-1" || lark.Text != "你好" {
		t.Fatalf("lark = %+v, err = %v", lark, err)
	}
}
