//go:build integration

package approval_test

// PG 版 Approval Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	"github.com/google/uuid"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/approval"
)

func TestPostgresApprovalStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runApprovalContract(t, func(t *testing.T) (approval.Store, string, string) {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `TRUNCATE approvals, checkpoints, conversations CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		// checkpoints.conversation_id 外键 → 先建一个会话。
		workspaceID := "approval-contract"
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, 'Approval Contract') ON CONFLICT (id) DO NOTHING`, workspaceID); err != nil {
			t.Fatalf("insert workspace: %v", err)
		}
		convID := uuid.New()
		if _, err := pool.Exec(ctx,
			`INSERT INTO conversations (id, workspace_id, status) VALUES ($1, $2, 'active')`, convID, workspaceID); err != nil {
			t.Fatalf("insert conversation: %v", err)
		}
		return approval.NewPostgresStore(pgstore.New(pool)), convID.String(), workspaceID
	})
}
