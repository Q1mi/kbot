package prompt_test

// Prompt Store 契约测试:memory 与 postgres 跑同一组用例(见 iam/contract_test.go 同款模式)。
// 只断言两实现共同子集;ID 在两实现间格式可能不同(PG 规范化为 canonical UUID),
// 故链式 get 用 store 回传/查得的 ID,不做字符串逐字节断言。

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/util"
)

func runPromptStoreContract(t *testing.T, newStore func(t *testing.T) prompt.Store) {
	ws := "ws-default"

	t.Run("PromptCreateGetList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		p := &domain.Prompt{ID: util.GenerateID(), WorkspaceID: ws, Name: "greet", Category: "support", CreatedBy: "u1"}
		if err := s.CreatePrompt(ctx, p); err != nil {
			t.Fatalf("CreatePrompt: %v", err)
		}
		got, err := s.GetPrompt(ctx, p.ID)
		if err != nil {
			t.Fatalf("GetPrompt: %v", err)
		}
		if got.Name != "greet" || got.Category != "support" || got.WorkspaceID != ws {
			t.Fatalf("prompt mismatch: %+v", got)
		}
		list, err := s.ListPrompts(ctx, ws)
		if err != nil {
			t.Fatalf("ListPrompts: %v", err)
		}
		if len(list) != 1 || list[0].Name != "greet" {
			t.Fatalf("ListPrompts mismatch: %+v", list)
		}
	})

	t.Run("VersionsImmutableAndQuery", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		p := &domain.Prompt{ID: util.GenerateID(), WorkspaceID: ws, Name: "p", CreatedBy: "u1"}
		if err := s.CreatePrompt(ctx, p); err != nil {
			t.Fatalf("CreatePrompt: %v", err)
		}
		v1 := &domain.PromptVersion{ID: util.GenerateID(), PromptID: p.ID, Version: 1, Template: "hi {{.name}}", VariablesSchema: `{"type":"object"}`, Hash: "h1", TokenEstimate: 3, CreatedBy: "u1"}
		v2 := &domain.PromptVersion{ID: util.GenerateID(), PromptID: p.ID, Version: 2, Template: "hello {{.name}}", VariablesSchema: "{}", Hash: "h2", TokenEstimate: 4, CreatedBy: "u1"}
		if err := s.CreatePromptVersion(ctx, v1); err != nil {
			t.Fatalf("CreatePromptVersion v1: %v", err)
		}
		if err := s.CreatePromptVersion(ctx, v2); err != nil {
			t.Fatalf("CreatePromptVersion v2: %v", err)
		}
		gotV1, err := s.GetPromptVersion(ctx, v1.ID)
		if err != nil {
			t.Fatalf("GetPromptVersion: %v", err)
		}
		if gotV1.Template != "hi {{.name}}" || gotV1.VariablesSchema != `{"type":"object"}` || gotV1.TokenEstimate != 3 {
			t.Fatalf("version mismatch: %+v", gotV1)
		}
		byNum, err := s.GetPromptVersionByNumber(ctx, p.ID, 2)
		if err != nil {
			t.Fatalf("GetPromptVersionByNumber: %v", err)
		}
		if byNum.Template != "hello {{.name}}" {
			t.Fatalf("byNumber mismatch: %+v", byNum)
		}
		all, err := s.ListPromptVersions(ctx, p.ID)
		if err != nil {
			t.Fatalf("ListPromptVersions: %v", err)
		}
		if len(all) != 2 || all[0].Version != 1 || all[1].Version != 2 {
			t.Fatalf("ListPromptVersions mismatch: %+v", all)
		}
	})

	t.Run("EnvBindingPromoteAndRead", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		p := &domain.Prompt{ID: util.GenerateID(), WorkspaceID: ws, Name: "p", CreatedBy: "u1"}
		_ = s.CreatePrompt(ctx, p)
		v := &domain.PromptVersion{ID: util.GenerateID(), PromptID: p.ID, Version: 1, Template: "x", VariablesSchema: "{}", Hash: "h", CreatedBy: "u1"}
		_ = s.CreatePromptVersion(ctx, v)

		if _, err := s.GetEnvBinding(ctx, p.ID, "prod"); err == nil {
			t.Fatal("expected error for unbound env")
		}
		if err := s.SetEnvBinding(ctx, p.ID, "prod", v.ID); err != nil {
			t.Fatalf("SetEnvBinding: %v", err)
		}
		boundID, err := s.GetEnvBinding(ctx, p.ID, "prod")
		if err != nil {
			t.Fatalf("GetEnvBinding: %v", err)
		}
		// ID 格式两实现可能不同,用查得的 ID 解引用验证指向 v。
		bound, err := s.GetPromptVersion(ctx, boundID)
		if err != nil {
			t.Fatalf("GetPromptVersion(bound): %v", err)
		}
		if bound.Hash != "h" || bound.Version != 1 {
			t.Fatalf("bound version mismatch: %+v", bound)
		}
	})

	t.Run("ExperimentUpsertAndActive", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		p := &domain.Prompt{ID: util.GenerateID(), WorkspaceID: ws, Name: "p", CreatedBy: "u1"}
		_ = s.CreatePrompt(ctx, p)

		// 无实验时返回 nil,nil。
		exp, err := s.GetActiveExperiment(ctx, p.ID, "prod")
		if err != nil || exp != nil {
			t.Fatalf("expected nil experiment, got %+v err=%v", exp, err)
		}
		started := time.Now()
		e := &domain.PromptExperiment{
			ID: util.GenerateID(), PromptID: p.ID, Env: "prod", Status: "active",
			Variants:  []domain.ExperimentVariant{{VersionID: "va", Traffic: 70}, {VersionID: "vb", Traffic: 30}},
			StartedAt: started,
		}
		if err := s.UpsertExperiment(ctx, e); err != nil {
			t.Fatalf("UpsertExperiment: %v", err)
		}
		got, err := s.GetActiveExperiment(ctx, p.ID, "prod")
		if err != nil || got == nil {
			t.Fatalf("expected active experiment, got %+v err=%v", got, err)
		}
		if len(got.Variants) != 2 || got.Variants[0].VersionID != "va" || got.Variants[0].Traffic != 70 {
			t.Fatalf("variants round-trip mismatch: %+v", got.Variants)
		}
		if got.StartedAt.Unix() != started.Unix() {
			t.Fatalf("started_at round-trip mismatch")
		}
		// upsert 改为 stopped → GetActive 返回 nil。
		e.Status = "stopped"
		if err := s.UpsertExperiment(ctx, e); err != nil {
			t.Fatalf("UpsertExperiment stop: %v", err)
		}
		got, err = s.GetActiveExperiment(ctx, p.ID, "prod")
		if err != nil || got != nil {
			t.Fatalf("expected nil after stop, got %+v err=%v", got, err)
		}
	})
}

func TestMemoryPromptStore_Contract(t *testing.T) {
	runPromptStoreContract(t, func(t *testing.T) prompt.Store {
		return platform.NewMemoryPromptStore()
	})
}
