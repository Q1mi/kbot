package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/tool"
)

func TestA2AExecutor(t *testing.T) {
	// 模拟一个 A2A v1 远端：AgentCard 声明 JSONRPC binding；/rpc 处理 SendMessage。
	mux := http.NewServeMux()
	var rpcURL string
	mux.HandleFunc("/card", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"name":"echo-agent","supportedInterfaces":[{"url":%q,"protocolBinding":"JSONRPC","protocolVersion":"1.0","tenant":"acme"}]}`, rpcURL)
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["method"] != "SendMessage" {
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}
		if r.Header.Get("A2A-Version") != "1.0" {
			http.Error(w, "missing version", http.StatusBadRequest)
			return
		}
		params, _ := req["params"].(map[string]any)
		if params["tenant"] != "acme" {
			http.Error(w, "missing tenant", http.StatusBadRequest)
			return
		}
		msg, _ := params["message"].(map[string]any)
		if msg["role"] != "ROLE_USER" || msg["messageId"] == "" {
			http.Error(w, "bad message", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"message":{"role":"ROLE_AGENT","parts":[{"text":"echo: hello"}],"messageId":"reply-1"}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	rpcURL = srv.URL + "/rpc"

	svc, _ := newToolService(t)
	ctx := context.Background()
	endpoint, _ := json.Marshal(map[string]string{"card_url": srv.URL + "/card"})
	tl, err := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "remote_echo", SourceType: "a2a",
		EndpointConfig: string(endpoint), CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create a2a tool: %v", err)
	}

	reg := NewRegistry(svc, fakeSandbox{})
	bt, err := reg.Build(ctx, tl.ID)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := bt.Executor.Execute(ctx, json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "echo: hello") {
		t.Fatalf("unexpected a2a result: %s", out)
	}
}

func TestA2AExecutorAcceptsFullMessage(t *testing.T) {
	var gotMessageID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/card":
			fmt.Fprintf(w, `{"name":"echo-agent","supportedInterfaces":[{"url":%q,"protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`, "http://"+r.Host+"/rpc")
		case "/rpc":
			var req struct {
				Params map[string]any `json:"params"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			msg, _ := req.Params["message"].(map[string]any)
			gotMessageID, _ = msg["messageId"].(string)
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"message":{"role":"ROLE_AGENT","parts":[{"text":"ok"}],"messageId":"reply-1"}}}`)
		}
	}))
	defer srv.Close()

	e := &a2aExecutor{client: srv.Client(), cardURL: srv.URL + "/card"}
	_, err := e.Execute(context.Background(), json.RawMessage(`{"message":{"role":"ROLE_USER","parts":[{"text":"hello"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotMessageID == "" {
		t.Fatal("expected executor to add messageId")
	}
}

func TestA2AMissingCardURL(t *testing.T) {
	svc, _ := newToolService(t)
	ctx := context.Background()
	tl, _ := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "bad_a2a", SourceType: "a2a",
		EndpointConfig: `{}`, CreatedBy: "u1",
	})
	reg := NewRegistry(svc, fakeSandbox{})
	bt, _ := reg.Build(ctx, tl.ID)
	if _, err := bt.Executor.Execute(ctx, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing card_url")
	}
}

func TestA2ARejectsCardWithoutJSONRPC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"name":"rest-only","supportedInterfaces":[{"url":"https://example.com/a2a","protocolBinding":"HTTP+JSON","protocolVersion":"1.0"}]}`)
	}))
	defer srv.Close()

	e := &a2aExecutor{client: srv.Client(), cardURL: srv.URL}
	if _, err := e.Execute(context.Background(), json.RawMessage(`{"message":"hello"}`)); err == nil {
		t.Fatal("expected JSONRPC interface error")
	}
}

func TestA2ARejectsCrossOriginInterface(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer target.Close()
	card := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"name":"unsafe","supportedInterfaces":[{"url":%q,"protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`, target.URL)
	}))
	defer card.Close()

	executor := &a2aExecutor{client: card.Client(), cardURL: card.URL}
	_, err := executor.Execute(context.Background(), json.RawMessage(`{"message":"hello"}`))
	if err == nil || !strings.Contains(err.Error(), "same origin") {
		t.Fatalf("expected cross-origin interface rejection, got %v", err)
	}
}
