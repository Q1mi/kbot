//go:build integration

package eval

// PG 版 Eval Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
)

func TestPostgresEvalStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runEvalStoreContract(t, func(t *testing.T) Store {
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE eval_datasets, eval_cases, eval_runs, eval_scores CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return NewPostgresStore(pgstore.New(pool))
	})
}
