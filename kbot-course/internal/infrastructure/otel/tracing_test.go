package otel

import "testing"

func TestAttributesCarryStableRunAndLangfuseDimensions(t *testing.T) {
	attributes := Attributes(RunContext{WorkspaceID: "ws", AgentVersionID: "agent-v1", ConversationID: "c1", UserID: "u1"})
	values := make(map[string]string, len(attributes))
	for _, item := range attributes {
		values[string(item.Key)] = item.Value.AsString()
	}
	for key, want := range map[string]string{"kbot.workspace.id": "ws", "langfuse.session.id": "c1", "langfuse.version": "agent-v1", "gen_ai.operation.name": "chat"} {
		if values[key] != want {
			t.Fatalf("%s = %q", key, values[key])
		}
	}
}
