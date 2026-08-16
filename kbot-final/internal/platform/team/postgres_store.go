package team

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 team.Store。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) CreateTeam(ctx context.Context, t *domain.Team) error {
	id, err := uuid.Parse(t.ID)
	if err != nil {
		return fmt.Errorf("parse team id: %w", err)
	}
	if _, err := s.q.CreateTeam(ctx, pgstore.CreateTeamParams{
		ID:          id,
		WorkspaceID: t.WorkspaceID,
		Name:        t.Name,
		Mode:        t.Mode,
	}); err != nil {
		return fmt.Errorf("create team: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateTeamWithVersion(ctx context.Context, t *domain.Team, v *domain.TeamVersion) error {
	teamID, err := uuid.Parse(t.ID)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	if err := s.q.CreateTeamWithVersion(ctx, pgstore.CreateTeamWithVersionParams{
		TeamID: teamID, WorkspaceID: t.WorkspaceID, Name: t.Name, Mode: t.Mode,
		VersionID: versionID, SnapshotJson: v.SnapshotJSON,
	}); err != nil {
		return fmt.Errorf("create team with version: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateTeamVersionAndBindDev(ctx context.Context, v *domain.TeamVersion) error {
	teamID, err := uuid.Parse(v.TeamID)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	return s.q.CreateTeamVersionAndBindDev(ctx, pgstore.CreateTeamVersionAndBindDevParams{
		VersionID: versionID, TeamID: teamID, Version: int32(v.Version), SnapshotJson: v.SnapshotJSON,
	})
}

func (s *PostgresStore) GetTeam(ctx context.Context, teamID string) (*domain.Team, error) {
	id, err := uuid.Parse(teamID)
	if err != nil {
		return nil, fmt.Errorf("parse team id: %w", err)
	}
	row, err := s.q.GetTeam(ctx, id)
	if err != nil {
		return nil, notFound("team", err)
	}
	return teamFromRow(row), nil
}

func (s *PostgresStore) ListTeams(ctx context.Context, workspaceID string) ([]*domain.Team, error) {
	rows, err := s.q.ListTeams(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	out := make([]*domain.Team, 0, len(rows))
	for _, r := range rows {
		out = append(out, teamFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) CreateTeamVersion(ctx context.Context, v *domain.TeamVersion) error {
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	teamID, err := uuid.Parse(v.TeamID)
	if err != nil {
		return fmt.Errorf("parse team id: %w", err)
	}
	if _, err := s.q.CreateTeamVersion(ctx, pgstore.CreateTeamVersionParams{
		ID:           id,
		TeamID:       teamID,
		Version:      int32(v.Version),
		SnapshotJson: v.SnapshotJSON,
	}); err != nil {
		return fmt.Errorf("create team version: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetTeamVersion(ctx context.Context, versionID string) (*domain.TeamVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse version id: %w", err)
	}
	row, err := s.q.GetTeamVersion(ctx, id)
	if err != nil {
		return nil, notFound("team version", err)
	}
	return teamVersionFromRow(row), nil
}

func (s *PostgresStore) ListTeamVersions(ctx context.Context, teamID string) ([]*domain.TeamVersion, error) {
	id, err := uuid.Parse(teamID)
	if err != nil {
		return nil, fmt.Errorf("parse team id: %w", err)
	}
	rows, err := s.q.ListTeamVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list team versions: %w", err)
	}
	out := make([]*domain.TeamVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, teamVersionFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) GetTeamCurrentVersion(ctx context.Context, teamID, env string) (*domain.TeamVersion, error) {
	id, err := uuid.Parse(teamID)
	if err != nil {
		return nil, fmt.Errorf("parse team id: %w", err)
	}
	row, err := s.q.GetTeamCurrentVersion(ctx, pgstore.GetTeamCurrentVersionParams{TeamID: id, Env: env})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no version for env %s", env)
		}
		return nil, fmt.Errorf("get current team version: %w", err)
	}
	return teamVersionFromRow(row), nil
}

func (s *PostgresStore) UpsertTeamEnv(ctx context.Context, teamID, env, versionID string) error {
	tid, err := uuid.Parse(teamID)
	if err != nil {
		return fmt.Errorf("parse team id: %w", err)
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	if err := s.q.UpsertTeamEnv(ctx, pgstore.UpsertTeamEnvParams{TeamID: tid, Env: env, VersionID: vid}); err != nil {
		return fmt.Errorf("upsert team env: %w", err)
	}
	return nil
}

func teamFromRow(r pgstore.Team) *domain.Team {
	return &domain.Team{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Mode:        r.Mode,
		CreatedAt:   r.CreatedAt,
	}
}

func teamVersionFromRow(r pgstore.TeamVersion) *domain.TeamVersion {
	return &domain.TeamVersion{
		ID:           r.ID.String(),
		TeamID:       r.TeamID.String(),
		Version:      int(r.Version),
		SnapshotJSON: r.SnapshotJson,
		CreatedAt:    r.CreatedAt,
	}
}

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}
