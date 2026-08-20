package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
)

func TestToolPolicyEnforcesNetworkAndKnowledgeBaseScopes(t *testing.T) {
	state := &agentRunState{
		policies: toolPolicies{
			requiresNetwork: map[string]bool{"remote": true},
			kbScoped:        map[string]bool{"search_kb": true},
		},
		agentKBs: map[string]bool{"kb-agent": true},
	}
	if err := state.toolPolicyError("remote", `{}`); err == nil || !strings.Contains(err.Error(), "requires network") {
		t.Fatalf("expected network denial, got %v", err)
	}
	state.allowNetwork = true
	if err := state.toolPolicyError("remote", `{}`); err != nil {
		t.Fatalf("authorized network tool rejected: %v", err)
	}
	if err := state.toolPolicyError("search_kb", `{"kb_id":"kb-other"}`); err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("expected agent KB denial, got %v", err)
	}
	if err := state.toolPolicyError("search_kb", `{"kb_id":"kb-agent"}`); err != nil {
		t.Fatalf("attached KB rejected: %v", err)
	}
	state.skills = []skillrunner.Spec{{Name: "refund", AllowedTools: []string{"search_kb"}, AllowedKBs: []string{"kb-skill"}}}
	state.activateSkill("refund")
	if err := state.toolPolicyError("search_kb", `{"kb_id":"kb-agent"}`); err == nil || !strings.Contains(err.Error(), "active skill") {
		t.Fatalf("expected skill KB denial, got %v", err)
	}
}

func TestDisableModelInvocationRequiresExplicitUserTrigger(t *testing.T) {
	specs := []skillrunner.Spec{{
		Name: "manual", Description: "manual", Body: "do it", DisableModelInvocation: true,
	}}
	if _, ok := explicitSkillFromMessages([]*schema.Message{schema.UserMessage("普通请求")}, specs); ok {
		t.Fatal("manual-only skill must stay hidden without an explicit user command")
	}
	name, ok := explicitSkillFromMessages([]*schema.Message{schema.UserMessage("/manual 处理任务")}, specs)
	if !ok || name != "manual" {
		t.Fatalf("explicit skill = %q, %v", name, ok)
	}
}

func TestRejectedParallelToolBatchCompletesEveryToolCall(t *testing.T) {
	first := schema.AssistantMessage("查询知识库", []schema.ToolCall{
		{ID: "call-invalid", Function: schema.FunctionCall{Name: "search_knowledge_base", Arguments: `{"kb_id":"guessed-kb"}`}},
		{ID: "call-valid", Function: schema.FunctionCall{Name: "search_knowledge_base", Arguments: `{"kb_id":"kb-agent"}`}},
	})
	e := &Engine{}
	policies := newToolPolicies()
	policies.kbScoped["search_knowledge_base"] = true
	events := make(chan AgentEvent, 8)
	state := &agentRunState{
		engine: e, policies: policies, agentKBs: map[string]bool{"kb-agent": true},
		emitter: emitter{ctx: context.Background(), ch: events},
	}
	handler := &runtimePolicyHandler{state: state}
	_, _, err := handler.AfterModelRewriteState(context.Background(), &adk.ChatModelAgentState{
		Messages: []*schema.Message{first},
	}, nil)
	if err != nil {
		t.Fatalf("classify batch: %v", err)
	}
	executed := 0
	endpoint := e.runtimeToolMiddleware(state).Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		executed++
		return &compose.ToolOutput{Result: "executed"}, nil
	})
	invalid, err := endpoint(context.Background(), &compose.ToolInput{
		Name: "search_knowledge_base", CallID: "call-invalid", Arguments: `{"kb_id":"guessed-kb"}`,
	})
	if err != nil || !strings.Contains(invalid.Result, "not attached") {
		t.Fatalf("invalid result=%+v err=%v", invalid, err)
	}
	valid, err := endpoint(context.Background(), &compose.ToolInput{
		Name: "search_knowledge_base", CallID: "call-valid", Arguments: `{"kb_id":"kb-agent"}`,
	})
	if err != nil || !strings.Contains(valid.Result, "batch rejected") {
		t.Fatalf("valid sibling result=%+v err=%v", valid, err)
	}
	if executed != 0 {
		t.Fatalf("batch policy allowed %d side effects", executed)
	}
}

func TestScopeKnowledgeBaseToolInfoAddsAllowedIDs(t *testing.T) {
	info := &schema.ToolInfo{
		Name: "search_knowledge_base",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"kb_id": {Type: schema.String, Desc: "Knowledge base ID", Required: true},
			"query": {Type: schema.String, Required: true},
		}),
	}
	scoped, err := scopeKnowledgeBaseToolInfo(info, []string{"kb-course"})
	if err != nil {
		t.Fatalf("scope tool info: %v", err)
	}
	params, err := scoped.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("to json schema: %v", err)
	}
	kbProperty, ok := params.Properties.Get("kb_id")
	if !ok || len(kbProperty.Enum) != 1 || kbProperty.Enum[0] != "kb-course" {
		t.Fatalf("kb_id schema = %+v", kbProperty)
	}
	if kbProperty.Default != "kb-course" || !strings.Contains(kbProperty.Description, "kb-course") {
		t.Fatalf("kb_id guidance = %+v", kbProperty)
	}
	original, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("original schema: %v", err)
	}
	originalKB, _ := original.Properties.Get("kb_id")
	if len(originalKB.Enum) != 0 {
		t.Fatal("scoping mutated registry tool info")
	}
}
