package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestA2AClientDiscoversCardAndSendsV1Message(t *testing.T) {
	mux := http.NewServeMux()
	var rpcURL string
	mux.HandleFunc("/card", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"name":"remote","supportedInterfaces":[{"url":"https://ignored.example/rpc","protocolBinding":"HTTP+JSON"},{"url":%q,"protocolBinding":"JSONRPC","protocolVersion":"1.0","tenant":"acme"}]}`, rpcURL)
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["method"] != "SendMessage" || r.Header.Get("A2A-Version") != "1.0" {
			t.Errorf("method=%v version=%q", request["method"], r.Header.Get("A2A-Version"))
		}
		params := request["params"].(map[string]any)
		message := params["message"].(map[string]any)
		metadata := params["metadata"].(map[string]any)
		if params["tenant"] != "acme" || message["role"] != "ROLE_USER" || message["messageId"] == "" || metadata["agent_version_id"] != "remote-v3" {
			t.Errorf("params=%+v", params)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"message": map[string]any{"role": "ROLE_AGENT", "parts": []map[string]string{{"text": "done"}}, "messageId": "reply-1"}}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	rpcURL = server.URL + "/rpc"

	client := NewA2AClient(server.URL+"/card", "127.0.0.1")
	client.client = server.Client()
	result, err := client.Send(context.Background(), "remote-v3", "analyze")
	if err != nil || result.Content != "done" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestA2AClientRejectsCardInterfaceOnAnotherHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"name":"remote","supportedInterfaces":[{"url":"http://metadata.internal/rpc","protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`)
	}))
	defer server.Close()
	client := NewA2AClient(server.URL, "127.0.0.1")
	client.client = server.Client()
	if _, err := client.Send(context.Background(), "remote-v1", "hello"); err == nil {
		t.Fatal("expected untrusted interface host to be rejected")
	}
}
