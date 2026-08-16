package team_test

// Team Store 契约测试:memory 与 postgres 跑同一组用例。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/team"
	"github.com/Q1mi/kbot/internal/util"
)

func runTeamStoreContract(t *testing.T, newStore func(t *testing.T) team.Store) {
	ws := "ws-default"

	t.Run("TeamCreateGetListVersionEnv", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		tm := &domain.Team{ID: util.GenerateID(), WorkspaceID: ws, Name: "research-squad", Mode: "supervisor"}
		if err := s.CreateTeam(ctx, tm); err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		got, err := s.GetTeam(ctx, tm.ID)
		if err != nil || got.Name != "research-squad" || got.Mode != "supervisor" {
			t.Fatalf("GetTeam mismatch: %+v err=%v", got, err)
		}
		list, err := s.ListTeams(ctx, ws)
		if err != nil || len(list) != 1 {
			t.Fatalf("ListTeams: %v len=%d", err, len(list))
		}

		v := &domain.TeamVersion{ID: util.GenerateID(), TeamID: tm.ID, Version: 1, SnapshotJSON: `{"members":[{"agent_id":"a1","role":"lead"}]}`}
		if err := s.CreateTeamVersion(ctx, v); err != nil {
			t.Fatalf("CreateTeamVersion: %v", err)
		}
		// 未绑定 env → 报错。
		if _, err := s.GetTeamCurrentVersion(ctx, tm.ID, "prod"); err == nil {
			t.Fatal("expected error for unbound env")
		}
		if err := s.UpsertTeamEnv(ctx, tm.ID, "prod", v.ID); err != nil {
			t.Fatalf("UpsertTeamEnv: %v", err)
		}
		cur, err := s.GetTeamCurrentVersion(ctx, tm.ID, "prod")
		if err != nil {
			t.Fatalf("GetTeamCurrentVersion: %v", err)
		}
		if cur.Version != 1 || cur.SnapshotJSON != `{"members":[{"agent_id":"a1","role":"lead"}]}` {
			t.Fatalf("current version mismatch: %+v", cur)
		}
	})
}

func TestMemoryTeamStore_Contract(t *testing.T) {
	runTeamStoreContract(t, func(t *testing.T) team.Store {
		return team.NewMemoryStore()
	})
}

// 服务层:CreateTeam(自动绑 dev)→ GetRunSpec 解析回 mode + members。
func TestMemoryTeamService_RunSpec(t *testing.T) {
	svc := team.NewService(team.NewMemoryStore(), nil) // nil resolver:本用例只验 store 往返,不 pin 版本
	ctx := context.Background()
	tm, _, err := svc.CreateTeam(ctx, team.CreateTeamRequest{
		WorkspaceID: "ws", Name: "squad", Mode: "pipeline",
		Members: []domain.TeamMember{{AgentID: "a1", Role: "researcher"}, {AgentID: "a2", Role: "writer"}},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	spec, err := svc.GetRunSpec(ctx, tm.ID, "ws", "dev")
	if err != nil {
		t.Fatalf("GetRunSpec: %v", err)
	}
	if spec.Mode != "pipeline" || len(spec.Members) != 2 || spec.Members[0].AgentID != "a1" || spec.Members[1].Role != "writer" {
		t.Fatalf("run spec mismatch: %+v", spec)
	}
}
