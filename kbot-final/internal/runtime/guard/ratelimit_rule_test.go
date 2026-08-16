package guard

import (
	"context"
	"testing"
	"time"
)

func TestRateLimitRule_BlocksOverLimit(t *testing.T) {
	rule := NewRateLimitRule(HookOnLLMCall, "llm", NewLimiter(nil, 2, time.Minute)) // 内存,2/窗口
	ctx := WithRateKey(context.Background(), "u1")

	if !rule.Check(ctx, "").Allow {
		t.Fatal("第 1 次应放行")
	}
	if !rule.Check(ctx, "").Allow {
		t.Fatal("第 2 次应放行")
	}
	if rule.Check(ctx, "").Allow {
		t.Fatal("第 3 次应被限流拦截")
	}

	// 不同身份各自计数,互不影响。
	ctx2 := WithRateKey(context.Background(), "u2")
	if !rule.Check(ctx2, "").Allow {
		t.Fatal("u2 首次应放行")
	}

	if rule.Hook() != HookOnLLMCall || rule.Name() != "rate_limit:llm" {
		t.Fatalf("hook/name mismatch: %s / %s", rule.Hook(), rule.Name())
	}
}

func TestGuard_OnLLMCall_RateLimited(t *testing.T) {
	g := New(nil).Add(NewRateLimitRule(HookOnLLMCall, "llm", NewLimiter(nil, 2, time.Minute)))
	ctx := WithRateKey(context.Background(), "u1")

	if err := g.OnLLMCall(ctx); err != nil {
		t.Fatalf("1st: %v", err)
	}
	if err := g.OnLLMCall(ctx); err != nil {
		t.Fatalf("2nd: %v", err)
	}
	err := g.OnLLMCall(ctx)
	if !IsBlocked(err) {
		t.Fatalf("3rd should be blocked, got %v", err)
	}
}

func TestMemoryLimiter_WindowReset(t *testing.T) {
	lim := newMemoryLimiter(1, time.Minute)
	cur := time.Now()
	lim.now = func() time.Time { return cur }
	ctx := context.Background()
	if ok, _ := lim.Allow(ctx, "k"); !ok {
		t.Fatal("1st should allow")
	}
	if ok, _ := lim.Allow(ctx, "k"); ok {
		t.Fatal("2nd in same window should block")
	}
	cur = cur.Add(2 * time.Minute) // 窗口过期
	if ok, _ := lim.Allow(ctx, "k"); !ok {
		t.Fatal("after window reset should allow again")
	}
}
