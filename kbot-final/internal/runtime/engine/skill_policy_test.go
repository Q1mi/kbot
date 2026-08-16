package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

func TestToolPolicyEnforcesNetworkAndKnowledgeBaseScopes(t *testing.T) {
	ls := &loopState{
		policies: toolPolicies{
			requiresNetwork: map[string]bool{"remote": true},
			kbScoped:        map[string]bool{"search_kb": true},
		},
		agentKBs: map[string]bool{"kb-agent": true},
	}
	if err := ls.toolPolicyError("remote", `{}`); err == nil || !strings.Contains(err.Error(), "requires network") {
		t.Fatalf("expected network denial, got %v", err)
	}
	ls.allowNetwork = true
	if err := ls.toolPolicyError("remote", `{}`); err != nil {
		t.Fatalf("authorized network tool rejected: %v", err)
	}
	if err := ls.toolPolicyError("search_kb", `{"kb_id":"kb-other"}`); err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("expected agent KB denial, got %v", err)
	}
	if err := ls.toolPolicyError("search_kb", `{"kb_id":"kb-agent"}`); err != nil {
		t.Fatalf("attached KB rejected: %v", err)
	}
	ls.activeSkill = &skillrunner.Spec{Name: "refund", AllowedKBs: []string{"kb-skill"}}
	ls.activeKBs = map[string]bool{"kb-skill": true}
	if err := ls.toolPolicyError("search_kb", `{"kb_id":"kb-agent"}`); err == nil || !strings.Contains(err.Error(), "active skill") {
		t.Fatalf("expected skill KB denial, got %v", err)
	}
}

func TestDisableModelInvocationRequiresExplicitUserTrigger(t *testing.T) {
	e := &Engine{}
	events := make(chan AgentEvent, 2)
	ls := &loopState{
		skills: []skillrunner.Spec{{
			Name: "manual", Description: "manual", Body: "do it", DisableModelInvocation: true,
		}},
	}
	em := emitter{ctx: context.Background(), ch: events}
	if !e.activateSkill(context.Background(), ls, "manual", true, em) {
		t.Fatal("model initiated denial should be handled")
	}
	if ls.activeSkill != nil {
		t.Fatal("model must not activate a manual-only skill")
	}
	if !e.activateSkill(context.Background(), ls, "manual", false, em) || ls.activeSkill == nil {
		t.Fatal("explicit user trigger should activate the skill")
	}
}

func TestRejectedParallelToolBatchCompletesEveryToolCall(t *testing.T) {
	first := schema.AssistantMessage("查询知识库", []schema.ToolCall{
		{ID: "call-invalid", Function: schema.FunctionCall{Name: "search_knowledge_base", Arguments: `{"kb_id":"guessed-kb"}`}},
		{ID: "call-valid", Function: schema.FunctionCall{Name: "search_knowledge_base", Arguments: `{"kb_id":"kb-agent"}`}},
	})
	gen := &scriptedGen{replies: []*schema.Message{first, schema.AssistantMessage("已识别知识库参数错误", nil)}}
	e := &Engine{gen: gen}
	policies := newToolPolicies()
	policies.kbScoped["search_knowledge_base"] = true
	ls := &loopState{
		messages:   []*schema.Message{schema.UserMessage("查询规则")},
		execByName: map[string]tooling.Executor{"search_knowledge_base": nil},
		policies:   policies,
		agentKBs:   map[string]bool{"kb-agent": true},
		maxSteps:   2,
	}
	events := make(chan AgentEvent, 8)
	answer, err := e.runLoop(context.Background(), ls, emitter{ctx: context.Background(), ch: events})
	if err != nil {
		t.Fatalf("run loop: %v", err)
	}
	if answer != "已识别知识库参数错误" {
		t.Fatalf("answer = %q", answer)
	}
	if len(gen.received) != 2 {
		t.Fatalf("generate calls = %d, want 2", len(gen.received))
	}
	secondRequest := gen.received[1]
	if err := validateToolCallResponses(secondRequest); err != nil {
		t.Fatalf("second request violates tool call contract: %v", err)
	}
	if len(secondRequest) != 4 {
		t.Fatalf("second request messages = %d, want 4", len(secondRequest))
	}
	if secondRequest[2].ToolCallID != "call-invalid" || !strings.Contains(secondRequest[2].Content, "not attached") {
		t.Fatalf("invalid call response = %+v", secondRequest[2])
	}
	if secondRequest[3].ToolCallID != "call-valid" || !strings.Contains(secondRequest[3].Content, "batch rejected") {
		t.Fatalf("valid call batch response = %+v", secondRequest[3])
	}
}

func TestValidateToolCallResponsesRejectsIncompleteBatch(t *testing.T) {
	assistant := schema.AssistantMessage("", []schema.ToolCall{
		{ID: "call-1", Function: schema.FunctionCall{Name: "one"}},
		{ID: "call-2", Function: schema.FunctionCall{Name: "two"}},
	})
	messages := []*schema.Message{assistant, schema.ToolMessage("ok", "call-1")}
	if err := validateToolCallResponses(messages); err == nil || !strings.Contains(err.Error(), "call-2") {
		t.Fatalf("expected missing call-2 error, got %v", err)
	}
	messages = append(messages, schema.ToolMessage("ok", "call-2"))
	if err := validateToolCallResponses(messages); err != nil {
		t.Fatalf("complete tool batch rejected: %v", err)
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
