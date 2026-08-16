package skill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 skill.Store。skill 无 env 指针,用 status(draft/published)。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

// ---- Skill ----

func (s *PostgresStore) CreateSkill(ctx context.Context, sk *domain.Skill) error {
	id, err := uuid.Parse(sk.ID)
	if err != nil {
		return fmt.Errorf("parse skill id: %w", err)
	}
	if _, err := s.q.CreateSkill(ctx, pgstore.CreateSkillParams{
		ID:          id,
		WorkspaceID: sk.WorkspaceID,
		Name:        sk.Name,
		Category:    sk.Category,
		CreatedBy:   sk.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create skill: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateSkillWithVersion(ctx context.Context, sk *domain.Skill, v *domain.SkillVersion) error {
	skillID, err := uuid.Parse(sk.ID)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	if err := s.q.CreateSkillWithVersion(ctx, pgstore.CreateSkillWithVersionParams{
		SkillID: skillID, WorkspaceID: sk.WorkspaceID, Name: sk.Name, Category: sk.Category,
		SkillCreatedBy: sk.CreatedBy, VersionID: versionID, FrontmatterJson: v.FrontmatterJSON,
		BodyMd: v.BodyMD, VersionStatus: v.Status, VersionCreatedBy: v.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create skill with version: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetSkill(ctx context.Context, skillID string) (*domain.Skill, error) {
	id, err := uuid.Parse(skillID)
	if err != nil {
		return nil, fmt.Errorf("parse skill id: %w", err)
	}
	row, err := s.q.GetSkill(ctx, id)
	if err != nil {
		return nil, notFound("skill", err)
	}
	return skillFromRow(row), nil
}

func (s *PostgresStore) ListSkills(ctx context.Context, workspaceID string) ([]*domain.Skill, error) {
	rows, err := s.q.ListSkills(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	out := make([]*domain.Skill, 0, len(rows))
	for _, r := range rows {
		out = append(out, skillFromRow(r))
	}
	return out, nil
}

// ---- SkillVersion ----

func (s *PostgresStore) CreateSkillVersion(ctx context.Context, v *domain.SkillVersion) error {
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	skillID, err := uuid.Parse(v.SkillID)
	if err != nil {
		return fmt.Errorf("parse skill id: %w", err)
	}
	if _, err := s.q.CreateSkillVersion(ctx, pgstore.CreateSkillVersionParams{
		ID:              id,
		SkillID:         skillID,
		Version:         int32(v.Version),
		FrontmatterJson: v.FrontmatterJSON,
		BodyMd:          v.BodyMD,
		Status:          v.Status,
		CreatedBy:       v.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create skill version: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetSkillVersion(ctx context.Context, versionID string) (*domain.SkillVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse version id: %w", err)
	}
	row, err := s.q.GetSkillVersion(ctx, id)
	if err != nil {
		return nil, notFound("skill version", err)
	}
	return skillVersionFromRow(row), nil
}

func (s *PostgresStore) ListSkillVersions(ctx context.Context, skillID string) ([]*domain.SkillVersion, error) {
	id, err := uuid.Parse(skillID)
	if err != nil {
		return nil, fmt.Errorf("parse skill id: %w", err)
	}
	rows, err := s.q.ListSkillVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list skill versions: %w", err)
	}
	out := make([]*domain.SkillVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, skillVersionFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) UpdateSkillVersionStatus(ctx context.Context, versionID, status string) error {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	if err := s.q.UpdateSkillVersionStatus(ctx, pgstore.UpdateSkillVersionStatusParams{ID: id, Status: status}); err != nil {
		return fmt.Errorf("update skill version status: %w", err)
	}
	return nil
}

// ---- Subscription ----

func (s *PostgresStore) CreateSubscription(ctx context.Context, sub *domain.SkillSubscription) error {
	skillID, err := uuid.Parse(sub.SkillID)
	if err != nil {
		return fmt.Errorf("parse skill id: %w", err)
	}
	versionID, err := uuid.Parse(sub.VersionID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	if err := s.q.CreateSkillSubscription(ctx, pgstore.CreateSkillSubscriptionParams{
		SkillID:     skillID,
		VersionID:   versionID,
		AgentID:     sub.AgentID,
		WorkspaceID: sub.WorkspaceID,
	}); err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListSubscriptionsForAgent(ctx context.Context, agentID string) ([]*domain.SkillSubscription, error) {
	rows, err := s.q.ListSubscriptionsForAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	out := make([]*domain.SkillSubscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, &domain.SkillSubscription{
			SkillID:     r.SkillID.String(),
			VersionID:   r.VersionID.String(),
			AgentID:     r.AgentID,
			WorkspaceID: r.WorkspaceID,
			CreatedAt:   r.CreatedAt,
		})
	}
	return out, nil
}

// ---- 行 → domain ----

func skillFromRow(r pgstore.Skill) *domain.Skill {
	return &domain.Skill{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Category:    r.Category,
		CreatedBy:   r.CreatedBy,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func skillVersionFromRow(r pgstore.SkillVersion) *domain.SkillVersion {
	return &domain.SkillVersion{
		ID:              r.ID.String(),
		SkillID:         r.SkillID.String(),
		Version:         int(r.Version),
		FrontmatterJSON: r.FrontmatterJson,
		BodyMD:          r.BodyMd,
		Status:          r.Status,
		CreatedBy:       r.CreatedBy,
		CreatedAt:       r.CreatedAt,
	}
}

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}
