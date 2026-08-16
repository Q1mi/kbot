package middleware

import "testing"

func TestWebSocketProtocolToken(t *testing.T) {
	if got := webSocketProtocolToken("kbot.v1, kbot.jwt.aaa.bbb.ccc"); got != "aaa.bbb.ccc" {
		t.Fatalf("token=%q", got)
	}
	if got := webSocketProtocolToken("kbot.v1"); got != "" {
		t.Fatalf("unexpected token=%q", got)
	}
}
