package insurance_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAgentAssetsPinToolsApprovalSkillAndEvals(t *testing.T) {
	raw, err := os.ReadFile("config/tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		Sensitive bool           `json:"sensitive"`
		Schema    map[string]any `json:"input_schema"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 3 {
		t.Fatalf("tools = %d", len(tools))
	}
	for _, tool := range tools {
		if tool.ID == "" || tool.Name == "" {
			t.Fatalf("invalid tool = %+v", tool)
		}
		if tool.Sensitive {
			if _, ok := tool.Schema["x-kbot-approval"]; !ok {
				t.Fatalf("sensitive tool %s has no approval metadata", tool.Name)
			}
		}
	}
	for _, path := range []string{"config/claim-skill.md", "config/agent.json", "config/eval-cases.json"} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("asset %s is unavailable", path)
		}
	}
}
