package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresStore struct{ pool *pgxpool.Pool }

func NewPostgresService(resolver AgentResolver, pool *pgxpool.Pool) *Service {
	service := NewService(resolver)
	service.postgres = &postgresStore{pool: pool}
	return service
}

func (s *postgresStore) create(ctx context.Context, workspaceID, name, mode string, members []Member) (Team, Version, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Team{}, Version{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Team
	if err := tx.QueryRow(ctx, `INSERT INTO teams (id,workspace_id,name,mode) VALUES (gen_random_uuid()::text,$1,$2,$3) RETURNING id,workspace_id,name,mode,created_at`, workspaceID, name, mode).
		Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Mode, &item.CreatedAt); err != nil {
		return Team{}, Version{}, err
	}
	raw, err := json.Marshal(members)
	if err != nil {
		return Team{}, Version{}, err
	}
	version := Version{TeamID: item.ID, Version: 1, Members: append([]Member(nil), members...), Environments: []string{"dev"}, CreatedAt: item.CreatedAt}
	if err := tx.QueryRow(ctx, `INSERT INTO team_versions (id,team_id,version,members) VALUES (gen_random_uuid()::text,$1,1,$2) RETURNING id,created_at`, item.ID, raw).Scan(&version.ID, &version.CreatedAt); err != nil {
		return Team{}, Version{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO team_promotions (team_id,environment,team_version_id) VALUES ($1,'dev',$2)`, item.ID, version.ID); err != nil {
		return Team{}, Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Team{}, Version{}, err
	}
	return item, version, nil
}

func (s *postgresStore) list(ctx context.Context, workspaceID string) ([]Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,mode,created_at FROM teams WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Team, 0)
	for rows.Next() {
		var item Team
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Mode, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *postgresStore) validateTeam(ctx context.Context, workspaceID, teamID string, members []Member) error {
	var mode string
	err := s.pool.QueryRow(ctx, `SELECT mode FROM teams WHERE workspace_id=$1 AND id=$2`, workspaceID, teamID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("team %s not found", teamID)
	}
	if err != nil {
		return err
	}
	return validateMembers(mode, members)
}

func (s *postgresStore) createVersion(ctx context.Context, workspaceID, teamID string, members []Member) (Version, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, teamID); err != nil {
		return Version{}, err
	}
	var next int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(v.version),0)+1 FROM team_versions v JOIN teams t ON t.id=v.team_id WHERE t.workspace_id=$1 AND t.id=$2`, workspaceID, teamID).Scan(&next); err != nil || next == 1 {
		if err == nil {
			err = fmt.Errorf("team %s not found", teamID)
		}
		return Version{}, err
	}
	raw, err := json.Marshal(members)
	if err != nil {
		return Version{}, err
	}
	version := Version{TeamID: teamID, Version: next, Members: append([]Member(nil), members...), Environments: []string{"dev"}}
	if err := tx.QueryRow(ctx, `INSERT INTO team_versions (id,team_id,version,members) VALUES (gen_random_uuid()::text,$1,$2,$3) RETURNING id,created_at`, teamID, next, raw).Scan(&version.ID, &version.CreatedAt); err != nil {
		return Version{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO team_promotions (team_id,environment,team_version_id) VALUES ($1,'dev',$2) ON CONFLICT (team_id,environment) DO UPDATE SET team_version_id=EXCLUDED.team_version_id,promoted_at=now()`, teamID, version.ID); err != nil {
		return Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, err
	}
	return version, nil
}

func (s *postgresStore) listVersions(ctx context.Context, workspaceID, teamID string) ([]Version, error) {
	rows, err := s.pool.Query(ctx, `SELECT v.id,v.team_id,v.version,v.members,v.created_at FROM team_versions v JOIN teams t ON t.id=v.team_id WHERE t.workspace_id=$1 AND t.id=$2 ORDER BY v.version`, workspaceID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Version, 0)
	for rows.Next() {
		var version Version
		var raw []byte
		if err := rows.Scan(&version.ID, &version.TeamID, &version.Version, &raw, &version.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &version.Members); err != nil {
			return nil, err
		}
		environments, err := s.environments(ctx, teamID, version.ID)
		if err != nil {
			return nil, err
		}
		version.Environments = environments
		result = append(result, version)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("team %s not found", teamID)
	}
	return result, rows.Err()
}

func (s *postgresStore) environments(ctx context.Context, teamID, versionID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT environment FROM team_promotions WHERE team_id=$1 AND team_version_id=$2 ORDER BY environment`, teamID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var environment string
		if err := rows.Scan(&environment); err != nil {
			return nil, err
		}
		result = append(result, environment)
	}
	return result, rows.Err()
}

func (s *postgresStore) promote(ctx context.Context, workspaceID, teamID, environment, versionID string) error {
	command, err := s.pool.Exec(ctx, `INSERT INTO team_promotions (team_id,environment,team_version_id)
		SELECT t.id,$3,v.id FROM teams t JOIN team_versions v ON v.team_id=t.id WHERE t.workspace_id=$1 AND t.id=$2 AND v.id=$4
		ON CONFLICT (team_id,environment) DO UPDATE SET team_version_id=EXCLUDED.team_version_id,promoted_at=now()`, workspaceID, teamID, environment, versionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("team or version not found")
	}
	return nil
}

func (s *postgresStore) runSpec(ctx context.Context, workspaceID, teamID, environment string) (RunSpec, error) {
	var mode string
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT t.mode,v.members FROM teams t JOIN team_promotions p ON p.team_id=t.id AND p.environment=$3 JOIN team_versions v ON v.id=p.team_version_id WHERE t.workspace_id=$1 AND t.id=$2`, workspaceID, teamID, environment).Scan(&mode, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunSpec{}, fmt.Errorf("team %s has no version in %s", teamID, environment)
	}
	if err != nil {
		return RunSpec{}, err
	}
	var members []Member
	if err := json.Unmarshal(raw, &members); err != nil {
		return RunSpec{}, err
	}
	return RunSpec{Mode: mode, Members: members}, nil
}
