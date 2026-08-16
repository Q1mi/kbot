//go:build integration

package tool_test

// PG 版 Tool Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/tool"
)

func TestPostgresToolStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runToolStoreContract(t, func(t *testing.T) tool.Store {
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE tools, tool_versions, tool_permissions, tool_test_runs CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return tool.NewPostgresStore(pgstore.New(pool))
	})
}
