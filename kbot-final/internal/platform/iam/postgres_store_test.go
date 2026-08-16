//go:build integration

package iam_test

// PG 版 IAM Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。
// 跑法:make test-integration,或
//   KBOT_TEST_DATABASE_URL=postgres://kbot:kbot@localhost:55432/kbot?sslmode=disable \
//     go test -tags=integration ./internal/platform/iam/
//
// 每个子用例用独立 schema(TRUNCATE 相关表)隔离,避免相互污染。

import (
	"context"
	"testing"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/iam"
)

func TestPostgresIAMStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runIAMStoreContract(t, func(t *testing.T) iam.Store {
		// 每个子用例清空相关表,保证用例间隔离。
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE users, workspaces, workspace_members, roles, user_roles, api_keys CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return iam.NewPostgresStore(pool, pgstore.New(pool))
	})
}
