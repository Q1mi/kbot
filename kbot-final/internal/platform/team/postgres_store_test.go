//go:build integration

package team_test

// PG 版 Team Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/team"
)

func TestPostgresTeamStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runTeamStoreContract(t, func(t *testing.T) team.Store {
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE teams, team_versions, team_envs CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return team.NewPostgresStore(pgstore.New(pool))
	})
}
