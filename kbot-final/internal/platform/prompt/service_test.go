package prompt_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
)

type failingVersionStore struct {
	prompt.Store
}

func (failingVersionStore) CreatePromptVersion(context.Context, *domain.PromptVersion) error {
	return errors.New("create version failed")
}

func newService() *prompt.Service {
	return prompt.NewService(platform.NewMemoryPromptStore(), promptcache.NewCache(), prompt.NoopPublisher{})
}

func TestCreatePromptBindsDevV1(t *testing.T) {
	svc := newService()
	ctx := context.Background()

	p, v, err := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "greeting",
		Template: "你好 {{.user_name}}", VariablesSchema: `{"required":["user_name"]}`,
		CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v.Version != 1 || v.TokenEstimate <= 0 {
		t.Fatalf("unexpected v1: %+v", v)
	}

	// dev 指针应指向 v1，渲染需要变量。
	got, err := svc.Render(ctx, p.ID, prompt.EnvDev, "user-x", map[string]any{"user_name": "小明"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "你好 小明" {
		t.Fatalf("unexpected render: %q", got)
	}
}

func TestCreatePromptPrevalidatesBeforeWrite(t *testing.T) {
	store := platform.NewMemoryPromptStore()
	svc := prompt.NewService(store, promptcache.NewCache(), prompt.NoopPublisher{})

	if _, _, err := svc.CreatePrompt(context.Background(), prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "invalid", Template: "你好 {{name}}", CreatedBy: "u1",
	}); err == nil {
		t.Fatal("expected invalid Go template variable to fail")
	}
	prompts, err := store.ListPrompts(context.Background(), "w1")
	if err != nil || len(prompts) != 0 {
		t.Fatalf("invalid create left orphan prompt: %+v err=%v", prompts, err)
	}
}

func TestCreatePromptRollsBackWhenVersionCreateFails(t *testing.T) {
	baseStore := platform.NewMemoryPromptStore()
	store := failingVersionStore{Store: baseStore}
	svc := prompt.NewService(store, promptcache.NewCache(), prompt.NoopPublisher{})

	if _, _, err := svc.CreatePrompt(context.Background(), prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "rollback", Template: "你好 {{.name}}", CreatedBy: "u1",
	}); err == nil {
		t.Fatal("expected publish failure")
	}
	prompts, err := baseStore.ListPrompts(context.Background(), "w1")
	if err != nil || len(prompts) != 0 {
		t.Fatalf("failed create left orphan prompt: %+v err=%v", prompts, err)
	}
}

func TestMissingVariableErrors(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	p, _, _ := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "g", Template: "你好 {{.user_name}}",
		VariablesSchema: `{"required":["user_name"]}`, CreatedBy: "u1",
	})
	_, err := svc.Render(ctx, p.ID, prompt.EnvDev, "u", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required var")
	}
}

func TestPromoteAndRollbackMovesPointerOnly(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	p, v1, _ := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "g", Template: "v1 内容", CreatedBy: "u1",
	})

	// 新增 v2（immutable，不改 dev 指针）。
	v2, err := svc.CreateVersion(ctx, p.ID, "v2 内容", "", "u1")
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	// dev 仍渲染 v1。
	got, _ := svc.Render(ctx, p.ID, prompt.EnvDev, "u", nil)
	if got != "v1 内容" {
		t.Fatalf("expected dev still v1, got %q", got)
	}

	// 晋升 v2 到 dev。
	if err := svc.Promote(ctx, p.ID, prompt.EnvDev, v2.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, _ = svc.Render(ctx, p.ID, prompt.EnvDev, "u", nil)
	if got != "v2 内容" {
		t.Fatalf("expected dev v2 after promote, got %q", got)
	}

	// 回滚到 v1（改指针）。
	if err := svc.Rollback(ctx, p.ID, prompt.EnvDev, v1.ID); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _ = svc.Render(ctx, p.ID, prompt.EnvDev, "u", nil)
	if got != "v1 内容" {
		t.Fatalf("expected dev back to v1 after rollback, got %q", got)
	}
}

func TestABExperimentDeterministicSplit(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	p, v1, _ := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "g", Template: "A", CreatedBy: "u1",
	})
	v2, _ := svc.CreateVersion(ctx, p.ID, "B", "", "u1")

	// 50/50 实验。
	_, err := svc.StartExperiment(ctx, p.ID, prompt.EnvDev, []domain.ExperimentVariant{
		{VersionID: v1.ID, Traffic: 50},
		{VersionID: v2.ID, Traffic: 50},
	})
	if err != nil {
		t.Fatalf("start experiment: %v", err)
	}

	// 同一 user 多次解析结果应稳定（确定性分流）。
	first, _ := svc.ResolveVersion(ctx, p.ID, prompt.EnvDev, "alice")
	for i := 0; i < 5; i++ {
		again, _ := svc.ResolveVersion(ctx, p.ID, prompt.EnvDev, "alice")
		if again != first {
			t.Fatalf("expected stable bucketing for alice, got %s then %s", first, again)
		}
	}

	// 两个变体都应被一些用户命中（分流真的在工作）。
	counts := map[string]int{}
	for _, u := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		vid, _ := svc.ResolveVersion(ctx, p.ID, prompt.EnvDev, u)
		counts[vid]++
	}
	if counts[v1.ID] == 0 || counts[v2.ID] == 0 {
		t.Fatalf("expected both variants to get traffic, got %+v", counts)
	}
}

func TestDiff(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	p, _, _ := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "g", Template: "line1\nline2\nline3", CreatedBy: "u1",
	})
	_, _ = svc.CreateVersion(ctx, p.ID, "line1\nline2-changed\nline3", "", "u1")

	diff, err := svc.Diff(ctx, p.ID, 1, 2)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(diff, "- line2") || !strings.Contains(diff, "+ line2-changed") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
	if !strings.Contains(diff, "  line1") {
		t.Fatalf("expected common line1 unchanged:\n%s", diff)
	}
}

func TestRolloutCompleteMakesCandidateBaseline(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	p, baseline, err := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "rollout", Template: "baseline", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := svc.CreateVersionConfigured(ctx, p.ID, "candidate", "{}", "profile-v1",
		domain.GenerationConfig{}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	exp, err := svc.StartRollout(ctx, p.ID, prompt.EnvDev, candidate.ID, 50, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if exp.BaselineVersionID != baseline.ID || exp.CandidateVersionID != candidate.ID {
		t.Fatalf("unexpected rollout: %+v", exp)
	}
	seenCandidate := false
	seenBaseline := false
	for _, user := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		resolved, err := svc.ResolveConfig(ctx, p.ID, prompt.EnvDev, user, nil)
		if err != nil {
			t.Fatal(err)
		}
		seenCandidate = seenCandidate || resolved.VersionID == candidate.ID
		seenBaseline = seenBaseline || resolved.VersionID == baseline.ID
	}
	if !seenCandidate || !seenBaseline {
		t.Fatalf("expected baseline and candidate traffic")
	}
	if err := svc.CompleteRollout(ctx, p.ID, prompt.EnvDev, "u1"); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveConfig(ctx, p.ID, prompt.EnvDev, "any-user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.VersionID != candidate.ID {
		t.Fatalf("candidate should become baseline, got %s", resolved.VersionID)
	}
}
