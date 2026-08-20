package approval

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type approvalRow interface{ Scan(...any) error }

const approvalColumns = `id,workspace_id,run_id,tool_call_id,tool_version_id,arguments,checkpoint,status,
decided_by,expires_at,lease_owner,lease_until,fencing_token,attempts,last_error,arguments_hash`

func (s *Service) createPostgres(ctx context.Context, request Request) (*Request, error) {
	row := s.pool.QueryRow(ctx, `INSERT INTO approval_requests
		(id,workspace_id,run_id,tool_call_id,tool_version_id,arguments,arguments_hash,checkpoint,status,expires_at)
		VALUES (gen_random_uuid()::text,$1,$2,$3,$4,$5,$6,$7,'pending',$8)
		RETURNING `+approvalColumns,
		request.WorkspaceID, request.RunID, request.ToolCallID, request.ToolVersionID,
		request.Arguments, request.argumentsHash[:], request.Checkpoint, request.ExpiresAt)
	return scanApproval(row)
}

func (s *Service) decidePostgres(ctx context.Context, workspaceID, requestID, actorID string, approved bool) error {
	status := StatusRejected
	if approved {
		status = StatusApproved
	}
	command, err := s.pool.Exec(ctx, `UPDATE approval_requests SET status=$4,decided_by=$3
		WHERE id=$2 AND workspace_id=$1 AND status='pending' AND expires_at>now()`, workspaceID, requestID, actorID, status)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("approval decision lost CAS or request expired")
	}
	return nil
}

func (s *Service) saveCheckpointPostgres(ctx context.Context, workspaceID, requestID string, checkpoint []byte) error {
	command, err := s.pool.Exec(ctx, `UPDATE approval_requests SET checkpoint=$3
		WHERE workspace_id=$1 AND id=$2 AND status='pending'`, workspaceID, requestID, checkpoint)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("approval checkpoint lost CAS")
	}
	return nil
}

func (s *Service) claimPostgres(ctx context.Context, workspaceID, requestID, runID, toolCallID, toolVersionID, workerID string, hash [32]byte, duration time.Duration) (*Lease, error) {
	row := s.pool.QueryRow(ctx, `UPDATE approval_requests SET
		status='executing',lease_owner=$8,lease_until=now()+$9::interval,
		fencing_token=fencing_token+1,attempts=attempts+1
		WHERE workspace_id=$1 AND id=$2 AND run_id=$3 AND tool_call_id=$4 AND tool_version_id=$5
		  AND arguments_hash=$6 AND expires_at>now()
		  AND (status='approved' OR (status='executing' AND lease_until<=now()))
		RETURNING `+approvalColumns,
		workspaceID, requestID, runID, toolCallID, toolVersionID, hash[:], workerID, duration.String())
	request, err := scanApproval(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("approval is not claimable or binding does not match")
	}
	if err != nil {
		return nil, err
	}
	return &Lease{Request: *request, Checkpoint: append([]byte(nil), request.Checkpoint...), Token: request.FencingToken}, nil
}

func (s *Service) completePostgres(ctx context.Context, workspaceID, requestID string, token uint64) error {
	command, err := s.pool.Exec(ctx, `UPDATE approval_requests SET status='completed',lease_owner='',lease_until=NULL
		WHERE workspace_id=$1 AND id=$2 AND status='executing' AND fencing_token=$3`, workspaceID, requestID, int64(token))
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("stale approval execution token")
	}
	return nil
}

func (s *Service) failPostgres(ctx context.Context, workspaceID, requestID string, token uint64, executionErr error, maxAttempts int) error {
	message := ""
	if executionErr != nil {
		message = executionErr.Error()
	}
	command, err := s.pool.Exec(ctx, `UPDATE approval_requests SET
		status=CASE WHEN attempts<$4 THEN 'approved' ELSE 'failed' END,
		lease_owner='',lease_until=NULL,last_error=$5
		WHERE workspace_id=$1 AND id=$2 AND status='executing' AND fencing_token=$3`,
		workspaceID, requestID, int64(token), maxAttempts, message)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("stale approval execution token")
	}
	return nil
}

func (s *Service) getPostgres(ctx context.Context, workspaceID, requestID string) (*Request, error) {
	request, err := scanApproval(s.pool.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approval_requests WHERE workspace_id=$1 AND id=$2`, workspaceID, requestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("approval %s not found", requestID)
	}
	return request, err
}

func (s *Service) listReadyPostgres(ctx context.Context, limit int) ([]Request, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+approvalColumns+` FROM approval_requests
		WHERE expires_at>now() AND (status='approved' OR (status='executing' AND lease_until<=now()))
		ORDER BY created_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Request, 0)
	for rows.Next() {
		request, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *request)
	}
	return result, rows.Err()
}

func scanApproval(row approvalRow) (*Request, error) {
	var request Request
	var lease pgtype.Timestamptz
	var fencing int64
	var hash []byte
	err := row.Scan(
		&request.ID, &request.WorkspaceID, &request.RunID, &request.ToolCallID, &request.ToolVersionID,
		&request.Arguments, &request.Checkpoint, &request.Status, &request.DecidedBy, &request.ExpiresAt,
		&request.LeaseOwner, &lease, &fencing, &request.Attempts, &request.LastError, &hash,
	)
	if err != nil {
		return nil, err
	}
	if lease.Valid {
		request.LeaseUntil = lease.Time
	}
	if fencing < 0 || len(hash) != sha256.Size {
		return nil, fmt.Errorf("invalid persisted approval state")
	}
	request.FencingToken = uint64(fencing)
	copy(request.argumentsHash[:], hash)
	return &request, nil
}
