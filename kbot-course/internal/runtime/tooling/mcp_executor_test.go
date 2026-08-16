package tooling

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
)

func TestExecutorDispatchesAuthenticatedMCPToolWithLifecycle(t *testing.T) {
	methods := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mcp-secret" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", "session-1")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-11-25"}}`, request.ID)
		case "notifications/initialized":
			if r.Header.Get("MCP-Session-Id") != "session-1" {
				t.Error("missing MCP session")
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"ok"}]}}`, request.ID)
		default:
			t.Fatalf("method = %q", request.Method)
		}
	}))
	defer server.Close()
	registry := platformtool.NewRegistry()
	config := fmt.Sprintf(`{"url":%q,"tool_name":"search"}`, server.URL)
	if err := registry.Register(t.Context(), platformtool.Version{ID: "mcp-v1", WorkspaceID: "ws", Name: "search", SourceType: "mcp_server", InputSchema: []byte(`{"type":"object"}`), Endpoint: server.URL, EndpointConfig: config, Published: true, AuthConfig: `{"headers":{"Authorization":"Bearer mcp-secret"}}`}); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry, server.Client(), "127.0.0.1")
	result, err := executor.Execute(t.Context(), Call{WorkspaceID: "ws", ToolVersionID: "mcp-v1", Arguments: json.RawMessage(`{"q":"kbot"}`)})
	if err != nil || len(result.Body) == 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if fmt.Sprint(methods) != "[initialize notifications/initialized tools/call]" {
		t.Fatalf("methods=%v", methods)
	}
}
