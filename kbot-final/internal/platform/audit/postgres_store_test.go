//go:build integration

package audit

// PG 版 Audit Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
)

func TestPostgresAuditStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runAuditStoreContract(t, func(t *testing.T) Store {
		if _, err := pool.Exec(context.Background(), `TRUNCATE audit_logs CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return NewPostgresStore(pgstore.New(pool))
	})
}
