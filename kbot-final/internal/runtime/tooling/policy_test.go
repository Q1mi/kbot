package tooling

import (
	"testing"

	"github.com/Q1mi/kbot/internal/platform/tool"
)

func TestToolPolicyMetadata(t *testing.T) {
	for _, source := range []string{"rest_api", "mcp_server", "a2a"} {
		if !sourceRequiresNetwork(source) {
			t.Fatalf("%s should require network", source)
		}
	}
	if sourceRequiresNetwork("internal_sdk") || sourceRequiresNetwork("code_execution") {
		t.Fatal("local tool types should not require network")
	}
	if !isKBScopedTool(&tool.ToolConfig{
		SourceType: "internal_sdk", EndpointConfig: map[string]interface{}{"sdk_name": "search_knowledge_base"},
	}) {
		t.Fatal("KB search tool must be scoped")
	}
}
