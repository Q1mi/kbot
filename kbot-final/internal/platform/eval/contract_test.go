package eval

// Eval Store 契约测试:memory 与 postgres 跑同一组用例。
// 内部测试包(package eval),与既有 eval_test.go 一致;memory/pg 工厂直接构造。
// 运行与分数也会读回，覆盖管理端历史查询需要的完整契约。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/util"
)

func runEvalStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	ws := "ws-default"

	t.Run("DatasetCRUD", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		d := &domain.EvalDataset{ID: util.GenerateID(), WorkspaceID: ws, Name: "golden", TargetKind: "agent"}
		if err := s.CreateDataset(ctx, d); err != nil {
			t.Fatalf("CreateDataset: %v", err)
		}
		got, err := s.GetDataset(ctx, d.ID)
		if err != nil || got.Name != "golden" || got.TargetKind != "agent" {
			t.Fatalf("GetDataset mismatch: %+v err=%v", got, err)
		}
		list, err := s.ListDatasets(ctx, ws)
		if err != nil || len(list) != 1 {
			t.Fatalf("ListDatasets: %v len=%d", err, len(list))
		}
	})

	t.Run("CasesAddList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		d := &domain.EvalDataset{ID: util.GenerateID(), WorkspaceID: ws, Name: "golden", TargetKind: "agent"}
		_ = s.CreateDataset(ctx, d)
		c1 := &domain.EvalCase{ID: util.GenerateID(), DatasetID: d.ID, Input: "你能做什么", Expected: "助手", Metadata: `{"tag":"smoke"}`}
		if err := s.AddCase(ctx, c1); err != nil {
			t.Fatalf("AddCase: %v", err)
		}
		cases, err := s.ListCases(ctx, d.ID)
		if err != nil || len(cases) != 1 {
			t.Fatalf("ListCases: %v len=%d", err, len(cases))
		}
		if cases[0].Input != "你能做什么" || cases[0].Expected != "助手" || cases[0].Metadata != `{"tag":"smoke"}` {
			t.Fatalf("case round-trip mismatch: %+v", cases[0])
		}
	})

	t.Run("RunAndScores", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		d := &domain.EvalDataset{ID: util.GenerateID(), WorkspaceID: ws, Name: "golden", TargetKind: "agent"}
		_ = s.CreateDataset(ctx, d)
		c := &domain.EvalCase{ID: util.GenerateID(), DatasetID: d.ID, Input: "q", Expected: "a"}
		_ = s.AddCase(ctx, c)
		run := &domain.EvalRun{ID: util.GenerateID(), DatasetID: d.ID, TargetID: util.GenerateID(), JudgeID: "", Status: "passed", PassRate: 0.9, Threshold: 0.8}
		if err := s.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		if err := s.AddScores(ctx, []*domain.EvalScore{
			{RunID: run.ID, CaseID: c.ID, Dimension: "accuracy", Score: 1, Reason: "matched"},
		}); err != nil {
			t.Fatalf("AddScores: %v", err)
		}
		storedRun, err := s.GetRun(ctx, run.ID)
		if err != nil || storedRun.Status != "passed" {
			t.Fatalf("GetRun: %+v err=%v", storedRun, err)
		}
		runs, err := s.ListRuns(ctx, d.ID)
		if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
			t.Fatalf("ListRuns: %+v err=%v", runs, err)
		}
		scores, err := s.ListScores(ctx, run.ID)
		if err != nil || len(scores) != 1 || scores[0].Reason != "matched" {
			t.Fatalf("ListScores: %+v err=%v", scores, err)
		}
	})
}

func TestMemoryEvalStore_Contract(t *testing.T) {
	runEvalStoreContract(t, func(t *testing.T) Store {
		return NewMemoryStore()
	})
}
