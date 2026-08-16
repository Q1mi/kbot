package skill_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/skill"
)

const sampleSkill = `---
name: refund-flow
description: 处理标准退款请求。当用户要求退款且订单未发货时使用。
allowed-tools: [crm.lookup_order, billing.refund]
allowed-kbs: [refund_policy]
---

## 流程
1. 用 crm.lookup_order 查订单
2. 用 billing.refund 发起退款
`

func TestParseSkill(t *testing.T) {
	fm, body, err := skill.ParseSkill(sampleSkill)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if fm.Name != "refund-flow" {
		t.Fatalf("unexpected name %q", fm.Name)
	}
	if len(fm.AllowedTools) != 2 || fm.AllowedTools[0] != "crm.lookup_order" {
		t.Fatalf("unexpected allowed-tools %+v", fm.AllowedTools)
	}
	if !strings.HasPrefix(body, "## 流程") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestParseSkillErrors(t *testing.T) {
	if _, _, err := skill.ParseSkill("no frontmatter here"); err != skill.ErrNoFrontmatter {
		t.Fatalf("expected ErrNoFrontmatter, got %v", err)
	}
	if _, _, err := skill.ParseSkill("---\nname: x\n(no closing)"); err != skill.ErrMalformed {
		t.Fatalf("expected ErrMalformed, got %v", err)
	}
}

// fakeChecker 让 allowed-tools 校验可控。
type fakeChecker struct{ exists map[string]bool }

func (f fakeChecker) ToolExistsByName(_ context.Context, _, name string) (bool, error) {
	return f.exists[name], nil
}

type fakeKBChecker struct{ exists map[string]bool }

func (f fakeKBChecker) KBExists(_ context.Context, _, id string) (bool, error) {
	return f.exists[id], nil
}

func TestPublishValidation(t *testing.T) {
	ctx := context.Background()
	checker := fakeChecker{exists: map[string]bool{"crm.lookup_order": true, "billing.refund": true}}
	svc := skill.NewService(platform.NewMemorySkillStore(), checker)

	_, v, err := svc.CreateSkill(ctx, skill.CreateSkillRequest{
		WorkspaceID: "w1", SkillMD: sampleSkill, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// allowed-tools 都存在 → 发布成功。
	if err := svc.Publish(ctx, v.ID); err != nil {
		t.Fatalf("expected publish success, got %v", err)
	}
}

func TestPublishRejectsMissingTool(t *testing.T) {
	ctx := context.Background()
	// billing.refund 不存在。
	checker := fakeChecker{exists: map[string]bool{"crm.lookup_order": true}}
	svc := skill.NewService(platform.NewMemorySkillStore(), checker)

	_, v, _ := svc.CreateSkill(ctx, skill.CreateSkillRequest{
		WorkspaceID: "w1", SkillMD: sampleSkill, CreatedBy: "u1",
	})
	if err := svc.Publish(ctx, v.ID); err == nil {
		t.Fatal("expected publish to fail for missing allowed-tool")
	}
}

func TestPublishRejectsMissingKnowledgeBase(t *testing.T) {
	ctx := context.Background()
	checker := fakeChecker{exists: map[string]bool{"crm.lookup_order": true, "billing.refund": true}}
	svc := skill.NewService(platform.NewMemorySkillStore(), checker).
		WithKBChecker(fakeKBChecker{exists: map[string]bool{}})
	_, version, err := svc.CreateSkill(ctx, skill.CreateSkillRequest{
		WorkspaceID: "w1", SkillMD: sampleSkill, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Publish(ctx, version.ID); err == nil || !strings.Contains(err.Error(), "allowed-kbs") {
		t.Fatalf("expected missing KB rejection, got %v", err)
	}
}

func TestSpecsForAgent(t *testing.T) {
	ctx := context.Background()
	svc := skill.NewService(platform.NewMemorySkillStore(), nil)

	sk, v, _ := svc.CreateSkill(ctx, skill.CreateSkillRequest{
		WorkspaceID: "w1", SkillMD: sampleSkill, CreatedBy: "u1",
	})
	if err := svc.Publish(ctx, v.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := svc.Subscribe(ctx, sk.ID, v.ID, "agent-1"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	specs, err := svc.SpecsForAgent(ctx, "agent-1")
	if err != nil {
		t.Fatalf("specs: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "refund-flow" {
		t.Fatalf("unexpected specs %+v", specs)
	}
	if len(specs[0].AllowedTools) != 2 {
		t.Fatalf("expected 2 allowed tools, got %+v", specs[0].AllowedTools)
	}
}
