//go:build integration

package llm

// PgModelCallSink 集成测试:写 model_call_logs(分区父表)并读回 cached_tokens。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
)

func TestPgModelCallSink_RecordsCachedTokens(t *testing.T) {
	pool := testpg.Start(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE model_call_logs`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	sink := NewPgModelCallSink(pool)
	sink.Record(ctx, CallUsage{
		Provider: "openai-compatible", Model: "deepseek-chat",
		InputTokens: 1200, OutputTokens: 80, CachedTokens: 1024, LatencyMs: 350, Status: "ok",
	})

	var model string
	var input, output, cached int
	var status, classification string
	err := pool.QueryRow(ctx,
		`SELECT model, input_tokens, output_tokens, cached_tokens, status, classification FROM model_call_logs LIMIT 1`).
		Scan(&model, &input, &output, &cached, &status, &classification)
	if err != nil {
		t.Fatalf("query back: %v", err)
	}
	if model != "deepseek-chat" || input != 1200 || output != 80 || cached != 1024 || status != "ok" || classification != "internal" {
		t.Fatalf("row mismatch: model=%s in=%d out=%d cached=%d status=%s class=%s", model, input, output, cached, status, classification)
	}
}
