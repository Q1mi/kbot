package skillrunner

import (
	"context"
	"strings"
	"testing"

	platformskill "github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

func courseSkills() []platformskill.Package {
	return []platformskill.Package{
		{Name: "refund-flow", Description: "处理退款", Instructions: "退款流程正文", AllowedTools: []string{"refund"}, MaxSteps: 4},
		{Name: "manual-report", Description: "手工报表", Instructions: "报表流程", DisableModelInvocation: true, MaxSteps: 4},
	}
}

func TestBackendAdaptsPinnedSkillPackages(t *testing.T) {
	backend := NewBackend(courseSkills())
	frontMatters, err := backend.List(context.Background())
	if err != nil || len(frontMatters) != 2 || frontMatters[0].Name != "refund-flow" {
		t.Fatalf("list: %+v err=%v", frontMatters, err)
	}
	loaded, err := backend.Get(context.Background(), "refund-flow")
	if err != nil || loaded.Content != "退款流程正文" {
		t.Fatalf("get: %+v err=%v", loaded, err)
	}
}

func TestNewRuntimeUsesOfficialMiddlewareAndExplicitSkill(t *testing.T) {
	runtime, err := NewRuntime(t.Context(), courseSkills(), []tooling.Binding{{Name: "refund"}}, "/skill manual-report", nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil || len(runtime.Handlers) != 2 || runtime.ExplicitName != "manual-report" {
		t.Fatalf("runtime = %+v", runtime)
	}
}

func TestPolicyRestrictsToolsNetworkAndKnowledgeBases(t *testing.T) {
	packages := []platformskill.Package{{
		Name: "safe", AllowedTools: []string{"remote", "search_knowledge_base"},
		AllowedKBs: []string{"kb-course"}, RequiresNetwork: true,
	}}
	bindings := []tooling.Binding{
		{Name: "remote", RequiresNetwork: true},
		{Name: "search_knowledge_base", KBScoped: true},
		{Name: "delete_user"},
	}
	policy := newPolicy(packages, bindings)
	if _, err := policy.Activate("safe"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize("remote", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize("delete_user", `{}`); err == nil {
		t.Fatal("expected undeclared tool to be rejected")
	}
	if err := policy.Authorize("search_knowledge_base", `{"kb_id":"kb-other"}`); err == nil {
		t.Fatal("expected cross-KB call to be rejected")
	}
	if err := policy.Authorize("search_knowledge_base", `{"kb_id":"kb-course"}`); err != nil {
		t.Fatal(err)
	}

	undeclared := newPolicy([]platformskill.Package{{Name: "unsafe", AllowedTools: []string{"remote"}}}, bindings)
	if _, err := undeclared.Activate("unsafe"); err == nil || !strings.Contains(err.Error(), "requires_network") {
		t.Fatalf("expected network declaration error, got %v", err)
	}
}
