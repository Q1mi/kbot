package tooling

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
)

type recordingSandbox struct {
	language string
	code     string
}

func (s *recordingSandbox) Run(_ context.Context, language, code string) (string, error) {
	s.language, s.code = language, code
	return "42\n", nil
}

func TestExecutorValidatesAndCallsPinnedRESTTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "run-1:call-1" {
			t.Errorf("missing idempotency key")
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing decrypted tool credential")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"order_id":"ORD-1"}` {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"status":"submitted"}`))
	}))
	defer server.Close()
	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{
		ID: "refund-v1", WorkspaceID: "ws-1", Name: "refund", Endpoint: server.URL,
		AuthConfig: `{"headers":{"Authorization":"Bearer secret"}}`,
		Published:  true, InputSchema: []byte(`{"type":"object","required":["order_id"]}`),
	})
	executor := NewExecutor(registry, server.Client(), "127.0.0.1")
	result, err := executor.Execute(context.Background(), Call{WorkspaceID: "ws-1", ToolVersionID: "refund-v1", Arguments: []byte(`{"order_id":"ORD-1"}`), IdempotencyKey: "run-1:call-1"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", result.StatusCode)
	}
}

func TestExecutorRejectsNonAllowlistedAndRedirectedHosts(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"secret":true}`))
	}))
	defer private.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privateURL := strings.Replace(private.URL, "127.0.0.1", "localhost", 1)
		http.Redirect(w, r, privateURL, http.StatusFound)
	}))
	defer redirect.Close()

	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{
		ID: "redirect-v1", WorkspaceID: "ws", Name: "redirect", Endpoint: redirect.URL,
		Published: true, InputSchema: []byte(`{"type":"object"}`),
	})
	redirectURL, _ := url.Parse(redirect.URL)
	executor := NewExecutor(registry, redirect.Client(), redirectURL.Hostname())
	_, err := executor.Execute(context.Background(), Call{WorkspaceID: "ws", ToolVersionID: "redirect-v1", Arguments: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestExecutorRejectsCrossOriginRedirectEvenWhenBothHostsAreAllowed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"secret":true}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(target.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer redirect.Close()

	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{
		ID: "redirect-v1", WorkspaceID: "ws", Name: "redirect", Endpoint: redirect.URL,
		Published: true, InputSchema: []byte(`{"type":"object"}`),
	})
	executor := NewExecutor(registry, redirect.Client(), "127.0.0.1", "localhost")
	_, err := executor.Execute(context.Background(), Call{WorkspaceID: "ws", ToolVersionID: "redirect-v1", Arguments: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestExecutorValidatesArgumentTypesAndAdditionalProperties(t *testing.T) {
	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{
		ID: "v1", WorkspaceID: "ws", Name: "tool", Endpoint: "http://example.com", Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"quantity":{"type":"integer"}},"required":["quantity"],"additionalProperties":false}`),
	})
	executor := NewExecutor(registry, nil, "example.com")
	for _, arguments := range []string{`{"quantity":"2"}`, `{"quantity":2,"extra":true}`} {
		if _, err := executor.Execute(context.Background(), Call{WorkspaceID: "ws", ToolVersionID: "v1", Arguments: []byte(arguments)}); err == nil {
			t.Fatalf("arguments %s passed schema validation", arguments)
		}
	}
}

func TestExecutorAppliesNestedJSONSchemaConstraints(t *testing.T) {
	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{
		ID: "v1", WorkspaceID: "ws", Name: "nested", Endpoint: "http://example.com", Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"order":{"type":"object","properties":{"id":{"type":"string","pattern":"^ORD-[0-9]+$"},"amount":{"type":"number","minimum":1}},"required":["id","amount"]}},"required":["order"]}`),
	})
	executor := NewExecutor(registry, nil, "example.com")
	for _, arguments := range []string{
		`{"order":{"id":"bad","amount":2}}`,
		`{"order":{"id":"ORD-1","amount":0}}`,
	} {
		if _, err := executor.Execute(context.Background(), Call{WorkspaceID: "ws", ToolVersionID: "v1", Arguments: []byte(arguments)}); err == nil {
			t.Fatalf("arguments %s passed nested schema validation", arguments)
		}
	}
}

func TestExecutorRunsRegisteredInternalSDKTool(t *testing.T) {
	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{
		ID: "search-v1", WorkspaceID: "ws", Name: "search_knowledge_base",
		SourceType: "internal_sdk", Endpoint: "search_knowledge_base", Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
	})
	executor := NewExecutor(registry, nil)
	executor.RegisterSDK("search_knowledge_base", func(_ context.Context, workspaceID string, arguments map[string]any) (Result, error) {
		if workspaceID != "ws" || arguments["query"] != "inventory" {
			t.Fatalf("scope/arguments = %s %#v", workspaceID, arguments)
		}
		return Result{StatusCode: http.StatusOK, Body: []byte(`[{"id":"passage-1"}]`)}, nil
	})
	result, err := executor.Execute(t.Context(), Call{WorkspaceID: "ws", ToolVersionID: "search-v1", Arguments: []byte(`{"query":"inventory"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Body), "passage-1") {
		t.Fatalf("result = %s", result.Body)
	}
}

func TestExecutorRejectsMissingRequiredArgument(t *testing.T) {
	registry := platformtool.NewRegistry()
	_ = registry.Register(context.Background(), platformtool.Version{ID: "v1", WorkspaceID: "ws", Name: "tool", Endpoint: "http://example.com", Published: true, InputSchema: []byte(`{"type":"object","required":["id"]}`)})
	_, err := NewExecutor(registry, nil).Execute(context.Background(), Call{WorkspaceID: "ws", ToolVersionID: "v1", Arguments: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExecutorRoutesCodeExecutionToSandboxRunner(t *testing.T) {
	registry := platformtool.NewRegistry()
	if err := registry.Register(context.Background(), platformtool.Version{
		ID: "python-v1", WorkspaceID: "ws", Name: "code_run_python",
		SourceType: "code_execution", Endpoint: "python", Published: true,
		InputSchema: []byte(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`),
	}); err != nil {
		t.Fatal(err)
	}
	runner := &recordingSandbox{}
	result, err := NewExecutor(registry, nil).WithSandbox(runner).Execute(context.Background(), Call{
		WorkspaceID: "ws", ToolVersionID: "python-v1", Arguments: []byte(`{"code":"print(6 * 7)"}`),
	})
	if err != nil {
		t.Fatalf("execute code tool: %v", err)
	}
	if runner.language != "python" || runner.code != "print(6 * 7)" || string(result.Body) != "42\n" {
		t.Fatalf("runner = %q / %q, result = %q", runner.language, runner.code, result.Body)
	}
}
