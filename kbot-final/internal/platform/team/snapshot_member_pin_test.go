package team_test

// 「不可变快照」贯彻到多 Agent 编排的教学用回归测试(与工具 pin 同源)。
//
// 建团队后,某个成员 agent 发了新版本,团队快照里 pin 的成员版本仍是【建团队那一刻】
// 的旧版本——老团队不随成员 agent 发新版而"换脑子"。
// 修复前 TeamMember 只存 agent_id、运行端实时取当前版本,这条用例会失败。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	pagent "github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/team"
	"github.com/Q1mi/kbot/internal/util"
)

func TestTeamPinsMemberAgentVersion(t *testing.T) {
	ctx := context.Background()

	agentStore := platform.NewMemoryAgentStore()
	teamStore := team.NewMemoryStore()
	// 成员 agent 无工具 → ToolResolver 传 nil;prompt/skills 此用例也用不到。
	agentSvc := pagent.NewService(agentStore, nil, nil, nil)
	// teamSvc 用 agentSvc 作为 AgentResolver,建团队时 pin 成员版本。
	teamSvc := team.NewService(teamStore, agentSvc)

	// 1) 建成员 agent → 自动有 v1(dev 当前版本)。
	ag, err := agentSvc.CreateAgent(ctx, pagent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "researcher", SystemPrompt: "你是研究员", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	v1, err := agentStore.GetAgentCurrentVersion(ctx, ag.ID, "dev")
	if err != nil {
		t.Fatalf("get agent v1: %v", err)
	}

	// 2) 建团队,成员引用该 agent → 此刻快照应 pin 死 v1 的版本 ID。
	tm, teamV1, err := teamSvc.CreateTeam(ctx, team.CreateTeamRequest{
		WorkspaceID: "w1", Name: "squad", Mode: "pipeline",
		Members: []domain.TeamMember{{AgentID: ag.ID, Role: "researcher"}}, // 只传 agent_id;pin 在 CreateTeam 内部完成
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// 3) 给成员 agent 发新版本 v2 → 成为 dev 当前版本。
	v2 := &domain.AgentVersion{
		ID: util.GenerateID(), AgentID: ag.ID, Version: 2,
		SnapshotJSON: `{"system_prompt":"你是升级版研究员"}`, CreatedBy: "u1",
	}
	if err := agentStore.CreateAgentVersion(ctx, v2); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if cur, _ := agentStore.GetAgentCurrentVersion(ctx, ag.ID, "dev"); cur.ID != v2.ID {
		t.Fatalf("precondition failed: current should be v2, got %s", cur.ID)
	}

	// 4) 解析团队 dev 当前版本的运行规格 → 成员必须仍 pin 在 v1,而非漂移到 v2。
	spec, err := teamSvc.GetRunSpec(ctx, tm.ID, "w1", "dev")
	if err != nil {
		t.Fatalf("GetRunSpec: %v", err)
	}
	if len(spec.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(spec.Members))
	}
	if got := spec.Members[0].AgentVersionID; got != v1.ID {
		t.Fatalf("团队成员版本漂移:pinned=%s 应为 v1=%s(v2=%s)", got, v1.ID, v2.ID)
	}

	// 5) 显式创建团队 v2 会重新按 dev pin 成员，并可提升到其他环境。
	teamV2, err := teamSvc.CreateTeamVersion(ctx, tm.ID, "w1", team.CreateTeamVersionRequest{
		AgentEnv: "dev", Members: []domain.TeamMember{{AgentID: ag.ID, Role: "researcher"}},
	})
	if err != nil {
		t.Fatalf("CreateTeamVersion: %v", err)
	}
	if teamV2.Version != 2 || teamV2.Members[0].AgentVersionID != v2.ID {
		t.Fatalf("team v2 pin mismatch: %+v", teamV2)
	}
	if err := teamSvc.PromoteTeamVersion(ctx, tm.ID, "w1", "prod", teamV1.ID); err != nil {
		t.Fatalf("promote team v1: %v", err)
	}
	prod, err := teamSvc.GetRunSpec(ctx, tm.ID, "w1", "prod")
	if err != nil || prod.Members[0].AgentVersionID != v1.ID {
		t.Fatalf("prod team binding mismatch: %+v err=%v", prod, err)
	}
}
