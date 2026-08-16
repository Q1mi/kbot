package guardconfig

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/runtime/guard"
)

func runGuardConfigContract(t *testing.T, newStore func(*testing.T) Store) {
	t.Helper()
	ctx := context.Background()
	service := NewService(newStore(t))
	created, err := service.CreateRule(ctx, "w1", Rule{
		Kind: "business_policy", Hook: "on_input", PatternOrModel: `(?i)secret`, Action: "redact", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := service.Rules(ctx, "w1", guard.HookOnInput)
	if err != nil || len(rules) != 1 {
		t.Fatalf("dynamic rules: len=%d err=%v", len(rules), err)
	}
	decision := rules[0].Check(ctx, "my secret")
	if decision.Patched != "my [REDACTED]" {
		t.Fatalf("redaction mismatch: %+v", decision)
	}
	runtimeGuard := guard.New(nil).WithRuleProvider(service)
	patched, err := runtimeGuard.OnInput(guard.WithWorkspaceKey(ctx, "w1"), "runtime secret")
	if err != nil || patched != "runtime [REDACTED]" {
		t.Fatalf("runtime dynamic rule mismatch: patched=%q err=%v", patched, err)
	}
	created.Enabled = false
	if _, err := service.UpdateRule(ctx, "w1", created.ID, *created); err != nil {
		t.Fatal(err)
	}
	rules, _ = service.Rules(ctx, "w1", guard.HookOnInput)
	if len(rules) != 0 {
		t.Fatalf("disabled rule still active: %+v", rules)
	}

	if _, err := service.SetQuota(ctx, "w1", "llm_calls", 2); err != nil {
		t.Fatal(err)
	}
	quotaGuard := guard.New(nil).Add(guard.NewQuotaRule("llm_calls", service))
	quotaCtx := guard.WithWorkspaceKey(ctx, "w1")
	if err := quotaGuard.OnLLMCall(quotaCtx); err != nil {
		t.Fatalf("first guarded quota call: %v", err)
	}
	if err := quotaGuard.OnLLMCall(quotaCtx); err != nil {
		t.Fatalf("second guarded quota call: %v", err)
	}
	if err := quotaGuard.OnLLMCall(quotaCtx); err == nil || !guard.IsBlocked(err) {
		t.Fatalf("third guarded quota call should block: %v", err)
	}

	// 重设额度会保留 used；换一个 workspace 验证 Service.Consume 的返回值。
	if _, err := service.SetQuota(ctx, "w2", "llm_calls", 2); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		allowed, _, err := service.Consume(ctx, "w2", "llm_calls", 1)
		if err != nil || !allowed {
			t.Fatalf("consume %d: allowed=%v err=%v", i, allowed, err)
		}
	}
	allowed, remaining, err := service.Consume(ctx, "w2", "llm_calls", 1)
	if err != nil || allowed || remaining != 0 {
		t.Fatalf("quota should block: allowed=%v remaining=%d err=%v", allowed, remaining, err)
	}
}

func TestMemoryStoreContract(t *testing.T) {
	runGuardConfigContract(t, func(*testing.T) Store { return NewMemoryStore() })
}
