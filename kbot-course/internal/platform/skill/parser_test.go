package skill

import "testing"

func TestParseSkillMD(t *testing.T) {
	pkg, err := ParseSkillMD([]byte(`---
name: refund-assistant
description: 处理退款申请
allowed_tools: [get_order, create_refund]
max_steps: 6
---
先查询订单，再按政策创建退款。`))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "refund-assistant" || pkg.MaxSteps != 6 || len(pkg.AllowedTools) != 2 {
		t.Fatalf("package = %+v", pkg)
	}
}

func TestParseSkillMDRejectsUnsafeStepLimit(t *testing.T) {
	_, err := ParseSkillMD([]byte("---\nname: x\ndescription: x\nmax_steps: 100\n---\nrun"))
	if err == nil {
		t.Fatal("expected max_steps validation")
	}
}

func TestParseSkillMDSupportsProductionHyphenatedMetadata(t *testing.T) {
	pkg, err := ParseSkillMD([]byte("---\nname: order_exception_recovery\ndescription: recover orders\nallowed-tools: [get_order]\nallowed-kbs: [kb-1]\nrequires_network: true\n---\nFollow the recovery SOP."))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.MaxSteps != 8 || len(pkg.AllowedTools) != 1 || len(pkg.AllowedKBs) != 1 || !pkg.RequiresNetwork {
		t.Fatalf("package = %#v", pkg)
	}
}
