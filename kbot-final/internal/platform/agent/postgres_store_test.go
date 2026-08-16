//go:build integration

package agent_test

// PG 版 Agent Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/agent"
)

func TestPostgresAgentStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runAgentStoreContract(t, func(t *testing.T) agent.Store {
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE agents, agent_versions, agent_envs, conversations, messages CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return agent.NewPostgresStore(pool, pgstore.New(pool))
	})
}
