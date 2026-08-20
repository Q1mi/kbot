package modelconfig

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestModelProfileIsImmutableAndWorkspaceScoped(t *testing.T) {
	registry := NewRegistry()
	profile := ProfileVersion{ID: "fast-v1", WorkspaceID: "ws-1", ClassificationMax: "internal", Deployments: []Deployment{{Provider: "openai-compatible", Model: "demo", BaseURL: "http://llm.local/v1"}}}
	if err := registry.Publish(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	profile.Deployments[0].Model = "mutated"
	resolved, err := registry.Resolve(context.Background(), "ws-1", "fast-v1")
	if err != nil || resolved.Deployments[0].Model != "demo" {
		t.Fatalf("profile=%+v err=%v", resolved, err)
	}
	if _, err := registry.Resolve(context.Background(), "ws-2", "fast-v1"); err == nil {
		t.Fatal("expected workspace isolation")
	}
}

func TestModelProfileEncryptsProviderAPIKeyAndRedactsList(t *testing.T) {
	registry := NewRegistry([]byte("course-model-credential-key"))
	profile := ProfileVersion{ID: "real-v1", WorkspaceID: "ws-1", ClassificationMax: "confidential", Deployments: []Deployment{{
		Provider: "deepseek", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1", APIKey: "sk-course-secret",
	}}}
	if err := registry.Publish(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	listed := registry.List(context.Background(), "ws-1")
	raw, _ := json.Marshal(listed)
	if len(listed) != 1 || !listed[0].Deployments[0].HasAPIKey || strings.Contains(string(raw), "sk-course-secret") {
		t.Fatalf("public profile leaked credentials: %s", raw)
	}
	resolved, err := registry.Resolve(context.Background(), "ws-1", "real-v1")
	if err != nil || resolved.Deployments[0].APIKey != "sk-course-secret" {
		t.Fatalf("resolved profile = %#v, %v", resolved, err)
	}
}
