package tooling

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeMCPTransport struct {
	methods       []string
	notifications []string
}

func (t *fakeMCPTransport) Request(_ context.Context, method string, params any) (json.RawMessage, error) {
	t.methods = append(t.methods, method)
	if method == "initialize" {
		return json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}`), nil
	}
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
}

func (t *fakeMCPTransport) Notify(_ context.Context, method string, _ any) error {
	t.notifications = append(t.notifications, method)
	return nil
}

func TestMCPClientUsesToolsCall(t *testing.T) {
	transport := &fakeMCPTransport{}
	result, err := NewMCPClient(transport).CallTool(context.Background(), "search", map[string]any{"q": "kbot"})
	if err != nil || !json.Valid(result) {
		t.Fatalf("methods=%v result=%s err=%v", transport.methods, result, err)
	}
	if got, want := transport.methods, []string{"initialize", "tools/call"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("request order=%v want=%v", got, want)
	}
	if len(transport.notifications) != 1 || transport.notifications[0] != "notifications/initialized" {
		t.Fatalf("notifications=%v", transport.notifications)
	}
}
