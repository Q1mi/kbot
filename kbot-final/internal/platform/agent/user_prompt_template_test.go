package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
)

func TestAgentSeparatesSystemAndUserPromptBindings(t *testing.T) {
	ctx := context.Background()
	promptService := prompt.NewService(platform.NewMemoryPromptStore(), promptcache.NewCache(), prompt.NoopPublisher{})
	systemPrompt, _, err := promptService.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "商品运营 System", Category: "product-system",
		Template: "你是商品运营 Agent。", VariablesSchema: `{"type":"object","additionalProperties":false}`,
		CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	userPrompt, userVersion, err := promptService.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "商品运营 User Template", Category: "product-user-template",
		Template:        "处理订单 {{.order_id}}，目标：{{.objective}}。",
		VariablesSchema: `{"type":"object","required":["order_id","objective"],"properties":{"order_id":{"type":"string"},"objective":{"type":"string"}},"additionalProperties":false}`,
		CreatedBy:       "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	service := agent.NewService(platform.NewMemoryAgentStore(), promptService, nil, nil)

	_, err = service.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "invalid-sources", Template: "custom",
		SystemPrompt: "literal", SystemPromptID: systemPrompt.ID, CreatedBy: "u1",
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one system prompt source") {
		t.Fatalf("expected mutually exclusive system prompt validation, got %v", err)
	}
	_, err = service.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "invalid-system", Template: "custom",
		SystemPromptID: userPrompt.ID, CreatedBy: "u1",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot reference a user prompt template") {
		t.Fatalf("expected system prompt role validation, got %v", err)
	}
	_, err = service.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "invalid-user", Template: "custom",
		UserPromptID: systemPrompt.ID, CreatedBy: "u1",
	})
	if err == nil || !strings.Contains(err.Error(), "must reference a user prompt template") {
		t.Fatalf("expected user prompt role validation, got %v", err)
	}

	created, err := service.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "product-agent", Template: "custom",
		SystemPromptID: systemPrompt.ID, UserPromptID: userPrompt.ID,
		PromptEnv: prompt.EnvDev, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	spec, err := service.GetUserPromptInputSpec(ctx, created.ID, "w1", prompt.EnvDev, "u1")
	if err != nil {
		t.Fatalf("get user prompt input spec: %v", err)
	}
	if !spec.Enabled || spec.PromptID != userPrompt.ID || spec.PromptVersionID != userVersion.ID || !strings.Contains(spec.VariablesSchema, "order_id") {
		t.Fatalf("input spec = %+v", spec)
	}

	conversation, err := service.CreateConversation(ctx, created.ID, "u1")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	rendered, versionID, err := service.PrepareUserMessage(
		ctx, conversation.ID, conversation.AgentVersionID, "u1", "优先保障 SLA", userVersion.ID,
		map[string]any{"order_id": "TTS-1001", "objective": "恢复履约"},
	)
	if err != nil {
		t.Fatalf("prepare user message: %v", err)
	}
	if versionID != userVersion.ID || !strings.Contains(rendered, "处理订单 TTS-1001") || !strings.Contains(rendered, "优先保障 SLA") {
		t.Fatalf("rendered user message = %q, version=%s", rendered, versionID)
	}
	loaded, err := service.LoadConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	var runtime domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(loaded.RuntimeConfigJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.UserPromptVersionID != userVersion.ID || runtime.UserPromptVariables["order_id"] != "TTS-1001" || runtime.RenderedUserPrompt != rendered {
		t.Fatalf("conversation runtime config = %+v", runtime)
	}
}
