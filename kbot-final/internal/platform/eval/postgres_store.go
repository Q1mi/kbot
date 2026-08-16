package eval

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 eval.Store。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) CreateDataset(ctx context.Context, d *domain.EvalDataset) error {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return fmt.Errorf("parse dataset id: %w", err)
	}
	if _, err := s.q.CreateEvalDataset(ctx, pgstore.CreateEvalDatasetParams{
		ID:          id,
		WorkspaceID: d.WorkspaceID,
		Name:        d.Name,
		TargetKind:  orEmpty(d.TargetKind, "agent"),
	}); err != nil {
		return fmt.Errorf("create dataset: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDataset(ctx context.Context, id string) (*domain.EvalDataset, error) {
	did, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse dataset id: %w", err)
	}
	row, err := s.q.GetEvalDataset(ctx, did)
	if err != nil {
		return nil, notFound("eval dataset", err)
	}
	return datasetFromRow(row), nil
}

func (s *PostgresStore) ListDatasets(ctx context.Context, workspaceID string) ([]*domain.EvalDataset, error) {
	rows, err := s.q.ListEvalDatasets(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list datasets: %w", err)
	}
	out := make([]*domain.EvalDataset, 0, len(rows))
	for _, r := range rows {
		out = append(out, datasetFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) AddCase(ctx context.Context, c *domain.EvalCase) error {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return fmt.Errorf("parse case id: %w", err)
	}
	datasetID, err := uuid.Parse(c.DatasetID)
	if err != nil {
		return fmt.Errorf("parse dataset id: %w", err)
	}
	if _, err := s.q.AddEvalCase(ctx, pgstore.AddEvalCaseParams{
		ID:        id,
		DatasetID: datasetID,
		Input:     c.Input,
		Expected:  c.Expected,
		Metadata:  c.Metadata,
	}); err != nil {
		return fmt.Errorf("add case: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListCases(ctx context.Context, datasetID string) ([]*domain.EvalCase, error) {
	id, err := uuid.Parse(datasetID)
	if err != nil {
		return nil, fmt.Errorf("parse dataset id: %w", err)
	}
	rows, err := s.q.ListEvalCases(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list cases: %w", err)
	}
	out := make([]*domain.EvalCase, 0, len(rows))
	for _, r := range rows {
		out = append(out, caseFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) CreateRun(ctx context.Context, r *domain.EvalRun) error {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return fmt.Errorf("parse run id: %w", err)
	}
	datasetID, err := uuid.Parse(r.DatasetID)
	if err != nil {
		return fmt.Errorf("parse dataset id: %w", err)
	}
	if _, err := s.q.CreateEvalRun(ctx, pgstore.CreateEvalRunParams{
		ID:         id,
		DatasetID:  datasetID,
		TargetID:   r.TargetID,
		JudgeID:    r.JudgeID,
		Status:     orEmpty(r.Status, "running"),
		PassRate:   r.PassRate,
		Threshold:  r.Threshold,
		FinishedAt: tsFromPtr(r.FinishedAt),
	}); err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetRun(ctx context.Context, runID string) (*domain.EvalRun, error) {
	id, err := uuid.Parse(runID)
	if err != nil {
		return nil, fmt.Errorf("parse run id: %w", err)
	}
	row, err := s.q.GetEvalRun(ctx, id)
	if err != nil {
		return nil, notFound("eval run", err)
	}
	return runFromRow(row), nil
}

func (s *PostgresStore) ListRuns(ctx context.Context, datasetID string) ([]*domain.EvalRun, error) {
	id, err := uuid.Parse(datasetID)
	if err != nil {
		return nil, fmt.Errorf("parse dataset id: %w", err)
	}
	rows, err := s.q.ListEvalRuns(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list eval runs: %w", err)
	}
	out := make([]*domain.EvalRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, runFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) AddScores(ctx context.Context, scores []*domain.EvalScore) error {
	for _, sc := range scores {
		runID, err := uuid.Parse(sc.RunID)
		if err != nil {
			return fmt.Errorf("parse run id: %w", err)
		}
		caseID, err := uuid.Parse(sc.CaseID)
		if err != nil {
			return fmt.Errorf("parse case id: %w", err)
		}
		if err := s.q.AddEvalScore(ctx, pgstore.AddEvalScoreParams{
			RunID:     runID,
			CaseID:    caseID,
			Dimension: sc.Dimension,
			Score:     sc.Score,
			Reason:    sc.Reason,
		}); err != nil {
			return fmt.Errorf("add score: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) ListScores(ctx context.Context, runID string) ([]*domain.EvalScore, error) {
	id, err := uuid.Parse(runID)
	if err != nil {
		return nil, fmt.Errorf("parse run id: %w", err)
	}
	rows, err := s.q.ListEvalScores(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list eval scores: %w", err)
	}
	out := make([]*domain.EvalScore, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.EvalScore{
			RunID: row.RunID.String(), CaseID: row.CaseID.String(), Dimension: row.Dimension,
			Score: row.Score, Reason: row.Reason,
		})
	}
	return out, nil
}

// ---- 行 → domain ----

func datasetFromRow(r pgstore.EvalDataset) *domain.EvalDataset {
	return &domain.EvalDataset{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		TargetKind:  r.TargetKind,
		CreatedAt:   r.CreatedAt,
	}
}

func caseFromRow(r pgstore.EvalCase) *domain.EvalCase {
	return &domain.EvalCase{
		ID:        r.ID.String(),
		DatasetID: r.DatasetID.String(),
		Input:     r.Input,
		Expected:  r.Expected,
		Metadata:  r.Metadata,
		CreatedAt: r.CreatedAt,
	}
}

func runFromRow(r pgstore.EvalRun) *domain.EvalRun {
	result := &domain.EvalRun{
		ID: r.ID.String(), DatasetID: r.DatasetID.String(), TargetID: r.TargetID, JudgeID: r.JudgeID,
		Status: r.Status, PassRate: r.PassRate, Threshold: r.Threshold, CreatedAt: r.CreatedAt,
	}
	if r.FinishedAt.Valid {
		finishedAt := r.FinishedAt.Time
		result.FinishedAt = &finishedAt
	}
	return result
}

// ---- 小工具 ----

func orEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

func tsFromPtr(p *time.Time) pgtype.Timestamptz {
	if p == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *p, Valid: true}
}
