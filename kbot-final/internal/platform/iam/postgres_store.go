package iam

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 生成的查询实现 iam.Store。
// 注意:domain 的 ID 是字符串(util.GenerateID 产 32 位 hex),PG 列是 UUID;
// uuid.Parse 接受无连字符的 32 位 hex,落库后以 canonical 形式回读——
// Create 会把 canonical ID 写回入参对象,保证后续 GetByID 自洽。
type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

// NewPostgresStore 创建 IAM 的 PG 存储。
func NewPostgresStore(db *pgxpool.Pool, q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{db: db, q: q}
}

var _ Store = (*PostgresStore)(nil)

// ---- User ----

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	row, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, notFound("user", err)
	}
	return userFromRow(row), nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	row, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return nil, notFound("user", err)
	}
	return userFromRow(row), nil
}

func (s *PostgresStore) CreateUser(ctx context.Context, user *domain.User) error {
	id, err := uuid.Parse(user.ID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}
	role := user.Role
	if role == "" {
		role = GlobalRoleMember
	}
	status := user.Status
	if status == "" {
		status = "active"
	}
	row, err := s.q.CreateUser(ctx, pgstore.CreateUserParams{
		ID:           id,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Name:         user.Name,
		Role:         role,
		Status:       status,
	})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	*user = *userFromRow(row) // 写回 canonical ID + 时间戳
	return nil
}

func (s *PostgresStore) SetUserRole(ctx context.Context, userID, role string) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}
	return s.q.SetUserRole(ctx, pgstore.SetUserRoleParams{ID: id, Role: role})
}

// ---- Workspace ----

func (s *PostgresStore) ListUsers(ctx context.Context, limit, offset int32) ([]*domain.User, error) {
	rows, err := s.q.ListUsers(ctx, pgstore.ListUsersParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]*domain.User, 0, len(rows))
	for _, r := range rows {
		out = append(out, userFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) ListWorkspaces(ctx context.Context, limit, offset int32) ([]*domain.Workspace, error) {
	rows, err := s.q.ListWorkspaces(ctx, pgstore.ListWorkspacesParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	out := make([]*domain.Workspace, 0, len(rows))
	for _, r := range rows {
		out = append(out, workspaceFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) ListUserWorkspaces(ctx context.Context, userID string, limit, offset int32) ([]*domain.Workspace, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	rows, err := s.q.ListUserWorkspaces(ctx, pgstore.ListUserWorkspacesParams{UserID: id, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list user workspaces: %w", err)
	}
	out := make([]*domain.Workspace, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) GetWorkspaceMember(ctx context.Context, workspaceID, userID string) (*domain.WorkspaceMember, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	row, err := s.q.GetWorkspaceMember(ctx, pgstore.GetWorkspaceMemberParams{WorkspaceID: workspaceID, UserID: id})
	if err != nil {
		return nil, notFound("workspace member", err)
	}
	return &domain.WorkspaceMember{WorkspaceID: row.WorkspaceID, UserID: row.UserID.String(), Email: row.Email, Name: row.Name, Role: row.Role, CreatedAt: row.CreatedAt}, nil
}

func (s *PostgresStore) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]*domain.WorkspaceMember, error) {
	rows, err := s.q.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	out := make([]*domain.WorkspaceMember, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.WorkspaceMember{WorkspaceID: row.WorkspaceID, UserID: row.UserID.String(), Email: row.Email, Name: row.Name, Role: row.Role, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (s *PostgresStore) UpsertWorkspaceMember(ctx context.Context, member *domain.WorkspaceMember) error {
	id, err := uuid.Parse(member.UserID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}
	row, err := s.q.UpsertWorkspaceMember(ctx, pgstore.UpsertWorkspaceMemberParams{WorkspaceID: member.WorkspaceID, UserID: id, Role: member.Role})
	if err != nil {
		return fmt.Errorf("upsert workspace member: %w", err)
	}
	member.CreatedAt = row.CreatedAt
	member.UserID = row.UserID.String()
	return nil
}

func (s *PostgresStore) DeleteWorkspaceMember(ctx context.Context, workspaceID, userID string) (bool, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return false, fmt.Errorf("parse user id: %w", err)
	}
	rows, err := s.q.DeleteWorkspaceMember(ctx, pgstore.DeleteWorkspaceMemberParams{WorkspaceID: workspaceID, UserID: id})
	return rows > 0, err
}

// UpsertWorkspaceMemberGuarded 在工作空间级行锁下完成 owner 授权与最后 owner 保护。
func (s *PostgresStore) UpsertWorkspaceMemberGuarded(
	ctx context.Context, member *domain.WorkspaceMember, allowOwnerManagement bool,
) error {
	if s.db == nil {
		return fmt.Errorf("membership transaction database is not configured")
	}
	uid, err := uuid.Parse(member.UserID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockWorkspace(ctx, tx, member.WorkspaceID); err != nil {
		return err
	}
	existingRole, exists, err := workspaceMemberRole(ctx, tx, member.WorkspaceID, uid)
	if err != nil {
		return err
	}
	if (member.Role == WorkspaceRoleOwner || exists && existingRole == WorkspaceRoleOwner) && !allowOwnerManagement {
		return ErrForbidden
	}
	if exists && existingRole == WorkspaceRoleOwner && member.Role != WorkspaceRoleOwner {
		owners, err := workspaceOwnerCount(ctx, tx, member.WorkspaceID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	row, err := s.q.WithTx(tx).UpsertWorkspaceMember(ctx, pgstore.UpsertWorkspaceMemberParams{
		WorkspaceID: member.WorkspaceID, UserID: uid, Role: member.Role,
	})
	if err != nil {
		return fmt.Errorf("upsert workspace member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	member.CreatedAt = row.CreatedAt
	member.UserID = row.UserID.String()
	return nil
}

// DeleteWorkspaceMemberGuarded 与角色调整共用同一工作空间锁，避免并发删除全部 owner。
func (s *PostgresStore) DeleteWorkspaceMemberGuarded(
	ctx context.Context, workspaceID, userID string, allowOwnerManagement bool,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("membership transaction database is not configured")
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		return false, fmt.Errorf("parse user id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return false, err
	}
	role, exists, err := workspaceMemberRole(ctx, tx, workspaceID, uid)
	if err != nil || !exists {
		return false, err
	}
	if role == WorkspaceRoleOwner {
		if !allowOwnerManagement {
			return false, ErrForbidden
		}
		owners, err := workspaceOwnerCount(ctx, tx, workspaceID)
		if err != nil {
			return false, err
		}
		if owners <= 1 {
			return false, ErrLastOwner
		}
	}
	rows, err := s.q.WithTx(tx).DeleteWorkspaceMember(ctx, pgstore.DeleteWorkspaceMemberParams{
		WorkspaceID: workspaceID, UserID: uid,
	})
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return rows > 0, nil
}

func lockWorkspace(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id = $1 FOR UPDATE`, workspaceID).Scan(&id); err != nil {
		return notFound("workspace", err)
	}
	return nil
}

func workspaceMemberRole(ctx context.Context, tx pgx.Tx, workspaceID string, userID uuid.UUID) (string, bool, error) {
	var role string
	err := tx.QueryRow(ctx, `
		SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

func workspaceOwnerCount(ctx context.Context, tx pgx.Tx, workspaceID string) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner'
	`, workspaceID).Scan(&count)
	return count, err
}

func (s *PostgresStore) GetWorkspaceByID(ctx context.Context, workspaceID string) (*domain.Workspace, error) {
	row, err := s.q.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		return nil, notFound("workspace", err)
	}
	return workspaceFromRow(row), nil
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, ws *domain.Workspace) error {
	row, err := s.q.CreateWorkspace(ctx, pgstore.CreateWorkspaceParams{
		ID:          ws.ID,
		Name:        ws.Name,
		Description: ws.Description,
		ParentID:    textFromPtr(ws.ParentID),
	})
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	*ws = *workspaceFromRow(row)
	return nil
}

func (s *PostgresStore) CreateWorkspaceWithOwner(ctx context.Context, ws *domain.Workspace, userID string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	if err := s.q.CreateWorkspaceWithOwner(ctx, pgstore.CreateWorkspaceWithOwnerParams{
		WorkspaceID: ws.ID, Name: ws.Name, Description: ws.Description, ParentID: textFromPtr(ws.ParentID), UserID: uid,
	}); err != nil {
		return fmt.Errorf("create workspace with owner: %w", err)
	}
	return nil
}

// ---- 行 → domain 映射 ----

func userFromRow(r pgstore.User) *domain.User {
	return &domain.User{
		ID:           r.ID.String(),
		Email:        r.Email,
		Name:         r.Name,
		Role:         r.Role,
		PasswordHash: r.PasswordHash,
		Status:       r.Status,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func workspaceFromRow(r pgstore.Workspace) *domain.Workspace {
	return &domain.Workspace{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		ParentID:    ptrFromText(r.ParentID),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// ---- 小工具 ----

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

func textFromPtr(p *string) pgtype.Text {
	if p == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *p, Valid: true}
}

func ptrFromText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}
