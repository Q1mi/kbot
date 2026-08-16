package approval

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 approval.Store(approvals + checkpoints 两表)。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) CreatePending(ctx context.Context, a *Approval) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return fmt.Errorf("parse approval id: %w", err)
	}
	payload := a.Payload
	if payload == "" {
		payload = "{}"
	}
	if _, err := s.q.CreateApproval(ctx, pgstore.CreateApprovalParams{
		ID:             id,
		WorkspaceID:    a.WorkspaceID,
		ConversationID: pgUUID(a.ConversationID),
		Action:         a.Action,
		Payload:        []byte(payload),
	}); err != nil {
		return fmt.Errorf("create approval: %w", err)
	}
	return nil
}

func (s *PostgresStore) Get(ctx context.Context, id string) (*Approval, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse approval id: %w", err)
	}
	row, err := s.q.GetApproval(ctx, uid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("approval not found")
		}
		return nil, fmt.Errorf("get approval: %w", err)
	}
	return approvalFromRow(row), nil
}

func (s *PostgresStore) ListPending(ctx context.Context, workspaceID string) ([]*Approval, error) {
	rows, err := s.q.ListPendingApprovals(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals: %w", err)
	}
	out := make([]*Approval, 0, len(rows))
	for _, r := range rows {
		out = append(out, approvalFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) ListByConversation(ctx context.Context, conversationID string) ([]*Approval, error) {
	convID, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}
	rows, err := s.q.ListApprovalsByConversation(ctx, pgtype.UUID{Bytes: convID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list approvals by conversation: %w", err)
	}
	out := make([]*Approval, 0, len(rows))
	for _, row := range rows {
		out = append(out, approvalFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) ResolvePending(ctx context.Context, id, workspaceID, status, approverID string) (*Approval, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse approval id: %w", err)
	}
	row, err := s.q.ResolvePendingApproval(ctx, pgstore.ResolvePendingApprovalParams{
		ID:          uid,
		WorkspaceID: workspaceID,
		Status:      status,
		ApproverID:  pgUUID(approverID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrAlreadyResolved
		}
		return nil, fmt.Errorf("resolve approval: %w", err)
	}
	return approvalFromRow(row), nil
}

func (s *PostgresStore) BeginExecution(ctx context.Context, id, conversationID string) (string, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return "", fmt.Errorf("parse approval id: %w", err)
	}
	token := uuid.New()
	_, err = s.q.BeginApprovalExecution(ctx, pgstore.BeginApprovalExecutionParams{
		ID: uid, ConversationID: pgUUID(conversationID), ExecutionToken: pgUUID(token.String()),
	})
	if err == pgx.ErrNoRows {
		return "", ErrExecutionUnavailable
	}
	if err != nil {
		return "", err
	}
	return token.String(), nil
}

func (s *PostgresStore) RenewExecution(ctx context.Context, id, token string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	rows, err := s.q.RenewApprovalExecution(ctx, pgstore.RenewApprovalExecutionParams{
		ID: uid, ExecutionToken: pgUUID(token),
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrExecutionUnavailable
	}
	return nil
}

func (s *PostgresStore) CompleteExecution(ctx context.Context, id, token string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	rows, err := s.q.CompleteApprovalExecution(ctx, pgstore.CompleteApprovalExecutionParams{
		ID: uid, ExecutionToken: pgUUID(token),
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrExecutionUnavailable
	}
	return nil
}

func (s *PostgresStore) FailExecution(ctx context.Context, id, token, message string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	rows, err := s.q.FailApprovalExecution(ctx, pgstore.FailApprovalExecutionParams{
		ID: uid, ExecutionToken: pgUUID(token), ExecutionError: message,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrExecutionUnavailable
	}
	return nil
}

func (s *PostgresStore) ListReadyResumes(ctx context.Context, limit int32) ([]*Approval, error) {
	rows, err := s.q.ListReadyApprovalResumes(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*Approval, 0, len(rows))
	for _, row := range rows {
		out = append(out, approvalFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) SaveCheckpoint(ctx context.Context, approvalID, conversationID string, state []byte) error {
	apprID, err := uuid.Parse(approvalID)
	if err != nil {
		return fmt.Errorf("parse approval id: %w", err)
	}
	convID, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	if len(state) == 0 {
		state = []byte("{}")
	}
	if err := s.q.CreateCheckpoint(ctx, pgstore.CreateCheckpointParams{
		ID:             uuid.New(),
		ConversationID: convID,
		ApprovalID:     pgtype.UUID{Bytes: apprID, Valid: true},
		StateSnapshot:  state,
	}); err != nil {
		return fmt.Errorf("create checkpoint: %w", err)
	}
	return nil
}

func (s *PostgresStore) CheckpointForApproval(ctx context.Context, approvalID, conversationID string) ([]byte, error) {
	apprID, err := uuid.Parse(approvalID)
	if err != nil {
		return nil, fmt.Errorf("parse approval id: %w", err)
	}
	convID, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}
	row, err := s.q.GetCheckpointForApproval(ctx, pgstore.GetCheckpointForApprovalParams{
		ApprovalID:     pgtype.UUID{Bytes: apprID, Valid: true},
		ConversationID: convID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no checkpoint")
		}
		return nil, fmt.Errorf("get checkpoint for approval: %w", err)
	}
	return row.StateSnapshot, nil
}

// ---- helpers ----

func approvalFromRow(r pgstore.Approval) *Approval {
	a := &Approval{
		ID:                r.ID.String(),
		WorkspaceID:       r.WorkspaceID,
		ConversationID:    uuidStr(r.ConversationID),
		Action:            r.Action,
		Payload:           string(r.Payload),
		Status:            r.Status,
		ApproverID:        uuidStr(r.ApproverID),
		CreatedAt:         r.CreatedAt,
		ExecutionStatus:   r.ExecutionStatus,
		ExecutionError:    r.ExecutionError,
		ExecutionAttempts: int(r.ExecutionAttempts),
		ExecutionToken:    uuidStr(r.ExecutionToken),
	}
	if r.ResolvedAt.Valid {
		t := r.ResolvedAt.Time
		a.ResolvedAt = &t
	}
	if r.ExecutionStartedAt.Valid {
		t := r.ExecutionStartedAt.Time
		a.ExecutionStartedAt = &t
	}
	if r.ExecutionCompletedAt.Valid {
		t := r.ExecutionCompletedAt.Time
		a.ExecutionCompletedAt = &t
	}
	if r.ExecutionLeaseUntil.Valid {
		t := r.ExecutionLeaseUntil.Time
		a.ExecutionLeaseUntil = &t
	}
	return a
}

func pgUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
