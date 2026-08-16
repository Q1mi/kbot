package skill_test

// Skill Store 契约测试:memory 与 postgres 跑同一组用例。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/util"
)

func runSkillStoreContract(t *testing.T, newStore func(t *testing.T) skill.Store) {
	ws := "ws-default"

	t.Run("SkillCreateGetList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		sk := &domain.Skill{ID: util.GenerateID(), WorkspaceID: ws, Name: "greet", Category: "support", CreatedBy: "u1"}
		if err := s.CreateSkill(ctx, sk); err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}
		got, err := s.GetSkill(ctx, sk.ID)
		if err != nil {
			t.Fatalf("GetSkill: %v", err)
		}
		if got.Name != "greet" || got.Category != "support" {
			t.Fatalf("skill mismatch: %+v", got)
		}
		list, err := s.ListSkills(ctx, ws)
		if err != nil {
			t.Fatalf("ListSkills: %v", err)
		}
		if len(list) != 1 || list[0].Name != "greet" {
			t.Fatalf("ListSkills mismatch: %+v", list)
		}
	})

	t.Run("VersionsAndStatus", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		sk := &domain.Skill{ID: util.GenerateID(), WorkspaceID: ws, Name: "s", CreatedBy: "u1"}
		_ = s.CreateSkill(ctx, sk)
		v1 := &domain.SkillVersion{ID: util.GenerateID(), SkillID: sk.ID, Version: 1, FrontmatterJSON: `{"name":"s"}`, BodyMD: "## 流程\n1. 问候", Status: "draft", CreatedBy: "u1"}
		v2 := &domain.SkillVersion{ID: util.GenerateID(), SkillID: sk.ID, Version: 2, FrontmatterJSON: `{"name":"s2"}`, BodyMD: "## 流程\n1. 改", Status: "draft", CreatedBy: "u1"}
		if err := s.CreateSkillVersion(ctx, v1); err != nil {
			t.Fatalf("CreateSkillVersion v1: %v", err)
		}
		if err := s.CreateSkillVersion(ctx, v2); err != nil {
			t.Fatalf("CreateSkillVersion v2: %v", err)
		}
		gotV1, err := s.GetSkillVersion(ctx, v1.ID)
		if err != nil {
			t.Fatalf("GetSkillVersion: %v", err)
		}
		if gotV1.FrontmatterJSON != `{"name":"s"}` || gotV1.BodyMD != "## 流程\n1. 问候" {
			t.Fatalf("version mismatch: %+v", gotV1)
		}
		all, err := s.ListSkillVersions(ctx, sk.ID)
		if err != nil {
			t.Fatalf("ListSkillVersions: %v", err)
		}
		if len(all) != 2 || all[0].Version != 1 || all[1].Version != 2 {
			t.Fatalf("ListSkillVersions mismatch: %+v", all)
		}
		if err := s.UpdateSkillVersionStatus(ctx, v2.ID, "published"); err != nil {
			t.Fatalf("UpdateSkillVersionStatus: %v", err)
		}
		got, _ := s.GetSkillVersion(ctx, v2.ID)
		if got.Status != "published" {
			t.Fatalf("status not updated: %+v", got)
		}
	})

	t.Run("Subscriptions", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		sk := &domain.Skill{ID: util.GenerateID(), WorkspaceID: ws, Name: "s", CreatedBy: "u1"}
		_ = s.CreateSkill(ctx, sk)
		v := &domain.SkillVersion{ID: util.GenerateID(), SkillID: sk.ID, Version: 1, FrontmatterJSON: "{}", BodyMD: "x", Status: "published", CreatedBy: "u1"}
		_ = s.CreateSkillVersion(ctx, v)

		agentID := util.GenerateID()
		if err := s.CreateSubscription(ctx, &domain.SkillSubscription{SkillID: sk.ID, VersionID: v.ID, AgentID: agentID, WorkspaceID: ws}); err != nil {
			t.Fatalf("CreateSubscription: %v", err)
		}
		subs, err := s.ListSubscriptionsForAgent(ctx, agentID)
		if err != nil {
			t.Fatalf("ListSubscriptionsForAgent: %v", err)
		}
		if len(subs) != 1 || subs[0].WorkspaceID != ws {
			t.Fatalf("subscriptions mismatch: %+v", subs)
		}
		// 用查得的 version_id 解引用,验证指向 v(ID 格式两实现可能不同)。
		bound, err := s.GetSkillVersion(ctx, subs[0].VersionID)
		if err != nil || bound.Version != 1 {
			t.Fatalf("subscription version deref mismatch: %+v err=%v", bound, err)
		}
		// 其他 agent 无订阅。
		other, err := s.ListSubscriptionsForAgent(ctx, util.GenerateID())
		if err != nil {
			t.Fatalf("ListSubscriptionsForAgent(other): %v", err)
		}
		if len(other) != 0 {
			t.Fatalf("expected no subs for other agent, got %d", len(other))
		}
	})
}

func TestMemorySkillStore_Contract(t *testing.T) {
	runSkillStoreContract(t, func(t *testing.T) skill.Store {
		return platform.NewMemorySkillStore()
	})
}
