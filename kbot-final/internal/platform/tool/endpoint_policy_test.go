package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestEndpointPolicyRejectsMCPStdioInControlPlane(t *testing.T) {
	policy := NewEndpointPolicy(nil, false)
	for _, endpoint := range []map[string]any{
		{"transport": "stdio", "command": "sh"},
		{"command": "sh"},
	} {
		err := policy.validateConfig(context.Background(), "mcp_server", endpoint)
		if err == nil || !strings.Contains(err.Error(), "stdio is disabled") {
			t.Fatalf("expected stdio isolation error, got %v", err)
		}
	}
}

func TestEndpointPolicyAllowsSameOriginRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "ok") })
	server := httptest.NewServer(mux)
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	client := NewEndpointPolicy([]string{parsed.Hostname()}, false).HTTPClient(2 * time.Second)
	response, err := client.Get(server.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestEndpointPolicyBlocksCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "should not be reached")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	parsed, _ := url.Parse(source.URL)
	client := NewEndpointPolicy([]string{parsed.Hostname()}, false).HTTPClient(2 * time.Second)
	_, err := client.Get(source.URL)
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Fatalf("expected cross-origin redirect rejection, got %v", err)
	}
}

func TestEndpointPolicyRequiresMCPHTTPURL(t *testing.T) {
	policy := NewEndpointPolicy(nil, false)
	err := policy.validateConfig(context.Background(), "mcp_server", map[string]any{"transport": "streamable_http"})
	if err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("expected missing URL error, got %v", err)
	}
}
