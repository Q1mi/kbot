package skillrunner

import (
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

func TestApplyRestrictsToolsAndSteps(t *testing.T) {
	applied, err := Apply("你是客服助手。", skill.Package{Name: "refund", Instructions: "按退款流程执行。", AllowedTools: []string{"refund"}, MaxSteps: 4}, []tooling.Binding{{Name: "refund", VersionID: "v1"}, {Name: "delete_user", VersionID: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Tools) != 1 || applied.Tools[0].Name != "refund" || applied.MaxSteps != 4 {
		t.Fatalf("applied = %+v", applied)
	}
	if !strings.Contains(applied.SystemPrompt, "按退款流程") {
		t.Fatalf("prompt = %q", applied.SystemPrompt)
	}
}

func TestApplyEnforcesNetworkAndKnowledgeBasePolicy(t *testing.T) {
	available := []tooling.Binding{
		{Name: "remote", RequiresNetwork: true},
		{Name: "search_knowledge_base", KBScoped: true},
	}
	if _, err := Apply("base", skill.Package{Name: "unsafe", Instructions: "run", AllowedTools: []string{"remote"}, MaxSteps: 2}, available); err == nil {
		t.Fatal("expected undeclared network access to fail")
	}
	applied, err := Apply("base", skill.Package{
		Name: "safe", Instructions: "run", AllowedTools: []string{"remote", "search_knowledge_base"},
		AllowedKBs: []string{"kb-policy"}, RequiresNetwork: true, MaxSteps: 2,
	}, available)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Tools[1].RestrictKBs || len(applied.Tools[1].AllowedKBs) != 1 || applied.Tools[1].AllowedKBs[0] != "kb-policy" {
		t.Fatalf("applied KB policy = %#v", applied.Tools[1])
	}
}

func TestL1HidesSkillsWithModelInvocationDisabled(t *testing.T) {
	l1 := L1([]skill.Package{
		{Name: "visible", Description: "visible to model"},
		{Name: "manual", Description: "explicit only", DisableModelInvocation: true},
	})
	if !strings.Contains(l1, "visible") || strings.Contains(l1, "manual") {
		t.Fatalf("L1 = %q", l1)
	}
	if selected, ok := Select("/skill manual", []skill.Package{{Name: "manual", DisableModelInvocation: true}}); !ok || selected.Name != "manual" {
		t.Fatal("manual skill should remain explicitly selectable")
	}
}
