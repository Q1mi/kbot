//go:build integration

package kb_test

// PG 版 KB Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/kb"
)

func TestPostgresKBStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runKBStoreContract(t, func(t *testing.T) kb.Store {
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE kbs, kb_documents, kb_ingest_jobs, connector_instances, kb_chunks CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return kb.NewPostgresStore(pgstore.New(pool))
	})
}
