package skillrunner

import (
	"strings"
	"testing"
)

func specs() []Spec {
	return []Spec{
		{Name: "refund-flow", Description: "处理退款", Body: "退款流程正文", AllowedTools: []string{"billing.refund"}},
		{Name: "report", Description: "生成报表", Body: "报表流程"},
	}
}

func TestBuildL1(t *testing.T) {
	l1 := BuildL1(specs())
	if !strings.Contains(l1, "refund-flow：处理退款") {
		t.Fatalf("L1 missing refund-flow: %s", l1)
	}
	if !strings.Contains(l1, "<USE_SKILL>") {
		t.Fatalf("L1 missing USE_SKILL instruction: %s", l1)
	}
	if BuildL1(nil) != "" {
		t.Fatal("expected empty L1 for no skills")
	}
}

func TestBuildL1HidesManualOnlySkills(t *testing.T) {
	l1 := BuildL1([]Spec{
		{Name: "manual", Description: "仅手动", DisableModelInvocation: true},
		{Name: "auto", Description: "可自动"},
	})
	if strings.Contains(l1, "manual") || !strings.Contains(l1, "auto") {
		t.Fatalf("unexpected L1: %s", l1)
	}
	if got := BuildL1([]Spec{{Name: "manual", DisableModelInvocation: true}}); got != "" {
		t.Fatalf("manual-only skills must not be disclosed, got %q", got)
	}
}

func TestDetect(t *testing.T) {
	name, ok := Detect("好的，我来处理。\n<USE_SKILL>refund-flow</USE_SKILL>")
	if !ok || name != "refund-flow" {
		t.Fatalf("detect failed: %q %v", name, ok)
	}
	if _, ok := Detect("普通回答，没有标记"); ok {
		t.Fatal("should not detect")
	}
}

func TestDetectExplicit(t *testing.T) {
	available := specs()
	for _, input := range []string{"/skill refund-flow 请退款", "/refund-flow 请退款"} {
		name, ok := DetectExplicit(input, available)
		if !ok || name != "refund-flow" {
			t.Fatalf("DetectExplicit(%q) = %q, %v", input, name, ok)
		}
	}
	if _, ok := DetectExplicit("/ghost", available); ok {
		t.Fatal("unknown explicit skill must be rejected")
	}
}

func TestFindAndL2(t *testing.T) {
	sp, ok := Find(specs(), "refund-flow")
	if !ok {
		t.Fatal("expected to find refund-flow")
	}
	msg := L2Message(sp)
	if !strings.Contains(msg, "退款流程正文") || !strings.Contains(msg, "[激活技能 refund-flow]") {
		t.Fatalf("unexpected L2 message: %s", msg)
	}
	if _, ok := Find(specs(), "nonexistent"); ok {
		t.Fatal("should not find nonexistent skill")
	}
}
