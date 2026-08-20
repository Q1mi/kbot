package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
)

func TestExecutorDispatchesA2AToolThroughSharedContract(t *testing.T) {
	mux := http.NewServeMux()
	var rpcURL string
	mux.HandleFunc("/card", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"name":"remote","supportedInterfaces":[{"url":%q,"protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`, rpcURL)
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"role":"ROLE_AGENT","parts":[{"text":"remote answer"}],"messageId":"reply-1"}}}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	rpcURL = server.URL + "/rpc"

	registry := platformtool.NewRegistry()
	if err := registry.Register(context.Background(), platformtool.Version{
		ID: "remote-v1", WorkspaceID: "ws-1", Name: "remote_agent", SourceType: "a2a",
		InputSchema: []byte(`{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`),
		Endpoint:    server.URL + "/card", Published: true,
	}); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry, server.Client(), "127.0.0.1")
	result, err := executor.Execute(context.Background(), Call{WorkspaceID: "ws-1", ToolVersionID: "remote-v1", Arguments: json.RawMessage(`{"message":"hello"}`)})
	if err != nil || result.StatusCode != http.StatusOK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if string(result.Body) == "" {
		t.Fatal("expected A2A result body")
	}
}
