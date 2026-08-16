package guardconfig

import (
	"context"
	"fmt"

	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostgresStore struct{ q *pgstore.Queries }

func NewPostgresStore(q *pgstore.Queries) *PostgresStore { return &PostgresStore{q: q} }

func (s *PostgresStore) CreateRule(ctx context.Context, rule *Rule) error {
	id, err := uuid.Parse(rule.ID)
	if err != nil {
		return err
	}
	_, err = s.q.CreateGuardRule(ctx, pgstore.CreateGuardRuleParams{
		ID: id, Kind: rule.Kind, Hook: rule.Hook, PatternOrModel: rule.PatternOrModel,
		Action: rule.Action, Enabled: rule.Enabled, WorkspaceID: rule.WorkspaceID,
	})
	return err
}

func (s *PostgresStore) GetRule(ctx context.Context, id string) (*Rule, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	row, err := s.q.GetGuardRule(ctx, parsed)
	if err != nil {
		return nil, err
	}
	return ruleFromRow(row), nil
}

func (s *PostgresStore) ListRules(ctx context.Context, workspaceID string) ([]*Rule, error) {
	rows, err := s.q.ListGuardRules(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]*Rule, 0, len(rows))
	for _, row := range rows {
		out = append(out, ruleFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) UpdateRule(ctx context.Context, rule *Rule) error {
	id, err := uuid.Parse(rule.ID)
	if err != nil {
		return err
	}
	_, err = s.q.UpdateGuardRule(ctx, pgstore.UpdateGuardRuleParams{
		ID: id, Kind: rule.Kind, Hook: rule.Hook, PatternOrModel: rule.PatternOrModel,
		Action: rule.Action, Enabled: rule.Enabled,
	})
	return err
}

func (s *PostgresStore) SetQuota(ctx context.Context, quota *Quota) error {
	row, err := s.q.SetQuotaLimit(ctx, pgstore.SetQuotaLimitParams{
		Dimension: quota.Dimension, Period: quota.Period, Lim: quota.Limit,
	})
	if err != nil {
		return err
	}
	quota.Used = row.Used
	quota.Limit = row.Lim
	return nil
}

func (s *PostgresStore) ListQuotas(ctx context.Context, workspaceID, period string) ([]*Quota, error) {
	rows, err := s.q.ListWorkspaceQuotas(ctx, pgstore.ListWorkspaceQuotasParams{WorkspaceID: workspaceID, Period: period})
	if err != nil {
		return nil, err
	}
	out := make([]*Quota, 0, len(rows))
	for _, row := range rows {
		out = append(out, quotaFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) ConsumeQuota(
	ctx context.Context, dimension, period string, amount int64,
) (*Quota, bool, error) {
	row, err := s.q.ConsumeQuota(ctx, pgstore.ConsumeQuotaParams{Dimension: dimension, Period: period, Amount: amount})
	if err == nil {
		return quotaFromRow(row), true, nil
	}
	if err != pgx.ErrNoRows {
		return nil, false, err
	}
	existing, getErr := s.q.GetQuota(ctx, pgstore.GetQuotaParams{Dimension: dimension, Period: period})
	if getErr == pgx.ErrNoRows {
		return nil, true, nil
	}
	if getErr != nil {
		return nil, false, fmt.Errorf("get quota after consume: %w", getErr)
	}
	return quotaFromRow(existing), false, nil
}

func ruleFromRow(row pgstore.GuardRule) *Rule {
	return &Rule{
		ID: row.ID.String(), Kind: row.Kind, Hook: row.Hook, PatternOrModel: row.PatternOrModel,
		Action: row.Action, Enabled: row.Enabled, WorkspaceID: row.WorkspaceID,
	}
}

func quotaFromRow(row pgstore.QuotaLedger) *Quota {
	return &Quota{Dimension: row.Dimension, Period: row.Period, Used: row.Used, Limit: row.Lim}
}

var _ Store = (*PostgresStore)(nil)
