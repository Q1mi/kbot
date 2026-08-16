package tooling

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/tool"
)

func TestMCPStdioExecutor(t *testing.T) {
	cfg := &tool.ToolConfig{
		EndpointConfig: map[string]interface{}{
			"transport": "stdio",
			"command":   os.Args[0],
			"args":      []interface{}{"-test.run=TestMCPHelperProcess", "--"},
			"env":       map[string]interface{}{"GO_WANT_MCP_HELPER": "1"},
			"tool_name": "echo",
		},
	}
	exec := newMCPExecutor(nil, cfg)
	out, err := exec.Execute(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "echo: hello" {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	for scanner.Scan() {
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		switch req.Method {
		case "initialize":
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}}`+"\n", req.ID)
		case "notifications/initialized":
			// Notification 没有响应。
		case "tools/call":
			args, _ := req.Params["arguments"].(map[string]any)
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"echo: %s"}],"isError":false}}`+"\n", req.ID, args["text"])
			_ = writer.Flush()
			os.Exit(0)
		}
		_ = writer.Flush()
	}
	os.Exit(0)
}

func TestMCPStreamableHTTPExecutorJSON(t *testing.T) {
	const sessionID = "session-123"
	var initialized, called, deleted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}}`)
		case "notifications/initialized":
			if r.Header.Get("MCP-Session-Id") != sessionID || r.Header.Get("MCP-Protocol-Version") != "2025-11-25" {
				http.Error(w, "missing session headers", http.StatusBadRequest)
				return
			}
			initialized = true
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			called = true
			if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				http.Error(w, "missing accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"sunny"}],"structuredContent":{"temp":22},"isError":false}}`)
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	exec := newMCPExecutor(srv.Client(), &tool.ToolConfig{
		EndpointConfig: map[string]interface{}{
			"transport": "streamable_http",
			"url":       srv.URL,
			"tool_name": "weather",
		},
	})
	out, err := exec.Execute(context.Background(), json.RawMessage(`{"city":"beijing"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !initialized || !called || !deleted {
		t.Fatalf("lifecycle incomplete: initialized=%v called=%v deleted=%v", initialized, called, deleted)
	}
	if !strings.Contains(out, "sunny") || !strings.Contains(out, `"temp":22`) {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestMCPStreamableHTTPExecutorSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\n")
			fmt.Fprint(w, `data: {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"from sse"}]}}`+"\n\n")
		}
	}))
	defer srv.Close()

	exec := newMCPExecutor(srv.Client(), &tool.ToolConfig{
		EndpointConfig: map[string]interface{}{
			"transport": "streamable_http",
			"url":       srv.URL,
			"tool_name": "echo",
		},
	})
	out, err := exec.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "from sse" {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestMCPToolError(t *testing.T) {
	_, err := formatMCPToolResult(json.RawMessage(`{"content":[{"type":"text","text":"bad input"}],"isError":true}`))
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("expected actionable tool error, got %v", err)
	}
}
