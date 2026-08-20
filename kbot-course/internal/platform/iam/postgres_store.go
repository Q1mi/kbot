package iam

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/domain"
)

// PostgresStore 让登录身份与 Workspace 成员关系跨进程、跨重启共享。
type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) CreateUser(ctx context.Context, user *domain.User) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, name, created_at) VALUES ($1,$2,$3,$4,$5)`,
		user.ID, user.Email, user.PasswordHash, user.Name, user.CreatedAt)
	return err
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `SELECT id,email,password_hash,name,created_at FROM users WHERE lower(email)=lower($1)`, email))
}

func (s *PostgresStore) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `SELECT id,email,password_hash,name,created_at FROM users WHERE id=$1`, id))
}

type userRow interface{ Scan(...any) error }

func (s *PostgresStore) scanUser(row userRow) (*domain.User, error) {
	var user domain.User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, workspace *domain.Workspace) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO workspaces (id,name,created_at) VALUES ($1,$2,$3)`, workspace.ID, workspace.Name, workspace.CreatedAt)
	return err
}

func (s *PostgresStore) AddMembership(ctx context.Context, membership *domain.Membership) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO workspace_memberships (user_id,workspace_id,role,created_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id,workspace_id) DO UPDATE SET role=EXCLUDED.role`,
		membership.UserID, membership.WorkspaceID, membership.Role, membership.CreatedAt)
	return err
}

func (s *PostgresStore) HasMembership(ctx context.Context, userID, workspaceID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_memberships WHERE user_id=$1 AND workspace_id=$2)`, userID, workspaceID).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) GetMembership(ctx context.Context, userID, workspaceID string) (*domain.Membership, error) {
	var membership domain.Membership
	err := s.pool.QueryRow(ctx, `SELECT user_id,workspace_id,role,created_at FROM workspace_memberships WHERE user_id=$1 AND workspace_id=$2`, userID, workspaceID).
		Scan(&membership.UserID, &membership.WorkspaceID, &membership.Role, &membership.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMembershipNotFound
	}
	return &membership, err
}

func (s *PostgresStore) ListUserWorkspaces(ctx context.Context, userID string) ([]*domain.Workspace, error) {
	rows, err := s.pool.Query(ctx, `SELECT w.id,w.name,w.created_at FROM workspaces w
		JOIN workspace_memberships m ON m.workspace_id=w.id WHERE m.user_id=$1 ORDER BY w.created_at,w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*domain.Workspace, 0)
	for rows.Next() {
		workspace := new(domain.Workspace)
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, workspace)
	}
	return result, rows.Err()
}

var _ Store = (*PostgresStore)(nil)
