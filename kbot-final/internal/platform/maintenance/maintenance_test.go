//go:build integration

package maintenance

// 分区运维集成测试：建月分区，并验证对象存储不可用时旧分区会保留。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/infrastructure/postgres"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
)

func TestMaintenance_PartitionLifecycle(t *testing.T) {
	pool := testpg.Start(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 隔离:清空两张按月分区表(含各自 default 分区)。否则共享 testpg 上其它测试(如 tests/integration
	// 的 e2e、prompt-cache 日志)写入的行可能落在 *_default,导致 EnsureUpcomingPartitions 因 default
	// 已有当月行而无法 attach 当月分区(SQLSTATE 23514)。与各 store 契约测试的 TRUNCATE 模式一致。
	for _, tbl := range []string{"audit_logs", "model_call_logs"} {
		if _, err := pool.Exec(ctx, `TRUNCATE `+tbl+` CASCADE`); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	s := NewService(pool, nil, 13)

	// 1) 建当前月份附近分区。
	if err := s.EnsureUpcomingPartitions(ctx, now); err != nil {
		t.Fatalf("EnsureUpcomingPartitions: %v", err)
	}
	parts, err := postgres.ListMonthlyPartitions(ctx, pool, "audit_logs")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(parts) < 3 {
		t.Fatalf("expected >=3 monthly partitions (上/本/下月), got %d", len(parts))
	}

	// 2) 造一个 14 个月前的分区 + 一行数据(应被归档窗口覆盖)。
	old := now.AddDate(0, -14, 0)
	if err := postgres.EnsureMonthlyPartition(ctx, pool, "audit_logs", old); err != nil {
		t.Fatalf("ensure old partition: %v", err)
	}
	oldTS := time.Date(old.Year(), old.Month(), 15, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO audit_logs (actor, action, created_at) VALUES ('sys','x',$1)`, oldTS); err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	oldName := "audit_logs_" + old.Format("2006_01")

	// 归档前:旧分区在。
	if !hasPartition(ctx, t, pool, oldName) {
		t.Fatalf("old partition %s should exist before archive", oldName)
	}

	// 3) 未配置归档存储时安全失败。
	n, err := s.ArchiveOldPartitions(ctx, now)
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("ArchiveOldPartitions error=%v want=%v", err, ErrArchiveUnavailable)
	}
	if n != 0 {
		t.Fatalf("expected 0 archived partitions, got %d", n)
	}
	// 归档失败后旧分区仍在，避免审计数据丢失。
	if !hasPartition(ctx, t, pool, oldName) {
		t.Fatalf("old partition %s should remain after archive failure", oldName)
	}
	// 当前月分区仍在。
	curName := "audit_logs_" + now.Format("2006_01")
	if !hasPartition(ctx, t, pool, curName) {
		t.Fatalf("current partition %s should remain", curName)
	}
}

func hasPartition(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var reg *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, name).Scan(&reg); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	return reg != nil
}
