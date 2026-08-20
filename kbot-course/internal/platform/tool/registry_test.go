package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistryPinsImmutablePublishedVersion(t *testing.T) {
	registry := NewRegistry()
	version := Version{ID: "refund-v1", WorkspaceID: "ws-1", Name: "refund", Published: true,
		InputSchema: []byte(`{"type":"object","required":["order_id"]}`)}
	if err := registry.Register(context.Background(), version); err != nil {
		t.Fatalf("register: %v", err)
	}
	version.InputSchema[0] = 'x'
	resolved, err := registry.Resolve(context.Background(), "ws-1", "refund-v1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(resolved.InputSchema) != `{"type":"object","required":["order_id"]}` {
		t.Fatalf("schema was mutated: %s", resolved.InputSchema)
	}
	if err := registry.Register(context.Background(), resolved); err == nil {
		t.Fatal("expected duplicate immutable version to fail")
	}
}

func TestRegistryEnforcesWorkspaceAndPublishState(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(context.Background(), Version{ID: "draft-v1", WorkspaceID: "ws-1", Name: "draft", InputSchema: []byte(`{"type":"object"}`)})
	if _, err := registry.Resolve(context.Background(), "ws-1", "draft-v1"); err == nil {
		t.Fatal("expected draft to be rejected")
	}
	if _, err := registry.Resolve(context.Background(), "ws-2", "draft-v1"); err == nil {
		t.Fatal("expected cross-workspace lookup to be rejected")
	}
}

func TestRegistryKeepsToolNameUniqueAndCredentialsSecret(t *testing.T) {
	registry := NewRegistry([]byte("course-only-credential-key"))
	first := Version{ID: "refund-v1", ToolID: "refund", WorkspaceID: "ws-1", Name: "refund", Published: true,
		InputSchema: []byte(`{"type":"object"}`), AuthConfig: `{"Authorization":"Bearer secret"}`}
	if err := registry.Register(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(context.Background(), Version{ID: "refund-v2", ToolID: "refund", WorkspaceID: "ws-1", Name: "refund", Published: true, InputSchema: []byte(`{"type":"object"}`)}); err != nil {
		t.Fatalf("new version of the same tool should be accepted: %v", err)
	}
	if err := registry.Register(context.Background(), Version{ID: "other-v1", ToolID: "other", WorkspaceID: "ws-1", Name: "Refund", Published: true, InputSchema: []byte(`{"type":"object"}`)}); err == nil {
		t.Fatal("expected duplicate workspace tool name to fail")
	}

	listed := registry.List(context.Background(), "ws-1")
	raw, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || !listed[0].HasAuth {
		t.Fatalf("public tool payload leaked credentials: %s", raw)
	}
	resolved, err := registry.Resolve(context.Background(), "ws-1", "refund-v1")
	if err != nil || !strings.Contains(resolved.AuthConfig, "secret") {
		t.Fatalf("runtime credentials were not restored: %#v, %v", resolved, err)
	}
}
