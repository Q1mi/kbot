package tooling

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
)

func TestExecutorDispatchesAuthenticatedMCPTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("Authorization") != "Bearer mcp-secret" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}}`, request.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"search","description":"search","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]}}`, request.ID)
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}`, request.ID)
		default:
			t.Fatalf("method = %q", request.Method)
		}
	}))
	defer server.Close()
	registry := platformtool.NewRegistry()
	if err := registry.Register(t.Context(), platformtool.Version{
		ID: "mcp-v1", WorkspaceID: "ws-1", Name: "mcp", SourceType: "mcp_server", Endpoint: server.URL,
		Published: true, AuthConfig: `{"headers":{"Authorization":"Bearer mcp-secret"}}`,
		InputSchema: []byte(`{"type":"object","required":["tool_name"],"properties":{"tool_name":{"type":"string"},"arguments":{"type":"object"}}}`),
	}); err != nil {
		t.Fatal(err)
	}
	result, err := NewExecutor(registry, server.Client(), "127.0.0.1").Execute(t.Context(), Call{
		WorkspaceID: "ws-1", ToolVersionID: "mcp-v1", Arguments: []byte(`{"tool_name":"search","arguments":{"q":"kbot"}}`),
	})
	if err != nil || result.StatusCode != http.StatusOK {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}
