package crossborder_test

import (
	"encoding/json"
	"os"
	"testing"
)

type toolManifest struct {
	Name       string         `json:"name"`
	Sensitive  bool           `json:"sensitive"`
	SchemaJSON map[string]any `json:"schema_json"`
}

func TestToolManifestHasUniqueNamesAndApprovalMetadata(t *testing.T) {
	raw, err := os.ReadFile("config/tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var tools []toolManifest
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) < 6 {
		t.Fatalf("tool manifest too small: %d", len(tools))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" || seen[tool.Name] {
			t.Fatalf("empty or duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Sensitive {
			if _, ok := tool.SchemaJSON["x-kbot-approval"]; !ok {
				t.Fatalf("sensitive tool %q lacks x-kbot-approval", tool.Name)
			}
		}
	}
}
