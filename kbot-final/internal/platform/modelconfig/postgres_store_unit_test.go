package modelconfig

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsProfileNameConflict(t *testing.T) {
	duplicate := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "model_profiles_workspace_id_name_key",
	}
	if !isProfileNameConflict(fmt.Errorf("wrapped: %w", duplicate)) {
		t.Fatal("expected wrapped profile name unique violation to be recognized")
	}
	if !isProfileNameConflict(duplicate) {
		t.Fatal("expected profile name unique violation to be recognized")
	}
	if isProfileNameConflict(&pgconn.PgError{Code: "23505", ConstraintName: "another_constraint"}) {
		t.Fatal("unrelated unique constraint was classified as a profile name conflict")
	}
}
