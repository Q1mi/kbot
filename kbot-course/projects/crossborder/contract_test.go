package crossborder_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSensitiveToolsDeclareApprovalMetadata(t *testing.T) {
	raw, err := os.ReadFile("config/tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Name      string         `json:"name"`
		Sensitive bool           `json:"sensitive"`
		Schema    map[string]any `json:"schema_json"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.Name == "" || seen[tool.Name] {
			t.Fatalf("empty or duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Sensitive {
			if _, ok := tool.Schema["x-kbot-approval"]; !ok {
				t.Fatalf("sensitive tool %q lacks approval metadata", tool.Name)
			}
		}
	}
}
