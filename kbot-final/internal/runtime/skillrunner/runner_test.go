package skillrunner

import (
	"context"
	"strings"
	"testing"
)

func specs() []Spec {
	return []Spec{
		{Name: "refund-flow", Description: "处理退款", Body: "退款流程正文", AllowedTools: []string{"billing.refund"}},
		{Name: "report", Description: "生成报表", Body: "报表流程"},
	}
}

func TestBackendAdaptsPinnedSpecs(t *testing.T) {
	backend := NewBackend(specs())
	frontMatters, err := backend.List(context.Background())
	if err != nil || len(frontMatters) != 2 || frontMatters[0].Name != "refund-flow" {
		t.Fatalf("list: %+v err=%v", frontMatters, err)
	}
	loaded, err := backend.Get(context.Background(), "refund-flow")
	if err != nil || loaded.Content != "退款流程正文" {
		t.Fatalf("get: %+v err=%v", loaded, err)
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
