//go:build integration

package guardconfig

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
)

func TestPostgresStoreContract(t *testing.T) {
	pool := testpg.Start(t)
	runGuardConfigContract(t, func(t *testing.T) Store {
		if _, err := pool.Exec(context.Background(), `TRUNCATE guard_rules, quota_ledger CASCADE`); err != nil {
			t.Fatalf("truncate guard config: %v", err)
		}
		return NewPostgresStore(pgstore.New(pool))
	})
}
