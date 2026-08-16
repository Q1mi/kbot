// Package iam 提供身份认证与权限管理
package iam

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/util"
)

var (
	ErrInvalidEmail = errors.New("invalid email")
	ErrWeakPassword = errors.New("password must be at least 8 characters")
	ErrNameRequired = errors.New("name is required")
	ErrForbidden    = errors.New("forbidden")
	ErrLastOwner    = errors.New("workspace must keep at least one owner")
)

const (
	GlobalRoleAdmin  = "admin"
	GlobalRoleMember = "member"

	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleEditor = "editor"
	WorkspaceRoleMember = "member"
	WorkspaceRoleViewer = "viewer"
)

// Service IAM服务
type Service struct {
	store  Store
	jwtKey []byte
}

// SeedWorkspace 描述课堂环境首启时应存在的业务工作空间。
type SeedWorkspace struct {
	Name        string
	Description string
}

// DefaultSeedWorkspaces 是课堂环境统一使用的两个独立业务项目。
var DefaultSeedWorkspaces = []SeedWorkspace{
	{Name: "跨境电商运营平台", Description: "跨境电商运营与供应链协同 Agent 课程项目"},
	{Name: "保险理赔与反欺诈平台", Description: "保险承保、理赔与反欺诈 Agent 课程项目"},
}

// NewService 创建IAM服务
func NewService(store Store, jwtKey []byte) *Service {
	return &Service{
		store:  store,
		jwtKey: jwtKey,
	}
}

// Store 定义IAM存储接口
type Store interface {
	// User相关
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
	ListUsers(ctx context.Context, limit, offset int32) ([]*domain.User, error)
	SetUserRole(ctx context.Context, userID, role string) error

	// Workspace相关
	GetWorkspaceByID(ctx context.Context, workspaceID string) (*domain.Workspace, error)
	CreateWorkspace(ctx context.Context, workspace *domain.Workspace) error
	ListWorkspaces(ctx context.Context, limit, offset int32) ([]*domain.Workspace, error)
	ListUserWorkspaces(ctx context.Context, userID string, limit, offset int32) ([]*domain.Workspace, error)
	GetWorkspaceMember(ctx context.Context, workspaceID, userID string) (*domain.WorkspaceMember, error)
	ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]*domain.WorkspaceMember, error)
	UpsertWorkspaceMember(ctx context.Context, member *domain.WorkspaceMember) error
	DeleteWorkspaceMember(ctx context.Context, workspaceID, userID string) (bool, error)
}

type workspaceOwnerStore interface {
	CreateWorkspaceWithOwner(context.Context, *domain.Workspace, string) error
}

// guardedMembershipStore 把 owner 权限校验、最后 owner 保护和成员写入放在同一临界区。
type guardedMembershipStore interface {
	UpsertWorkspaceMemberGuarded(context.Context, *domain.WorkspaceMember, bool) error
	DeleteWorkspaceMemberGuarded(context.Context, string, string, bool) (bool, error)
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      *domain.User `json:"user"`
}

// Claims JWT声明
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// Login 用户登录
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(req.Email))
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	if user.Status != "active" {
		return nil, fmt.Errorf("user account is not active")
	}

	// 生成JWT
	expiresAt := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "kbot",
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtKey)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return &LoginResponse{
		Token:     tokenString,
		ExpiresAt: expiresAt,
		User:      user,
	}, nil
}

// ValidateToken 验证JWT token
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("kbot"))

	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

// CreateUser 创建用户
func (s *Service) CreateUser(ctx context.Context, email, password, name string) (*domain.User, error) {
	email = normalizeEmail(email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return nil, ErrInvalidEmail
	}
	if len(password) < 8 {
		return nil, ErrWeakPassword
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		ID:           util.GenerateID(),
		Email:        email,
		Name:         name,
		Role:         GlobalRoleMember,
		PasswordHash: string(hashedPassword),
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// EnsureSeedAdmin 首启引导:若指定邮箱的用户不存在,则创建一个 admin 账号。
// 让学员 `make up && open localhost:8080` 即可直接登录,无需先跑 make seed。
// 幂等：已存在则原样返回，不修改密码。
func (s *Service) EnsureSeedAdmin(ctx context.Context, email, password string) error {
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		user, err = s.CreateUser(ctx, email, password, "Admin")
		if err != nil {
			return fmt.Errorf("ensure seed admin: %w", err)
		}
	}
	if err := s.store.SetUserRole(ctx, user.ID, GlobalRoleAdmin); err != nil {
		return fmt.Errorf("grant seed admin role: %w", err)
	}
	return nil
}

// EnsureSeedWorkspaces 幂等创建课堂环境的两个默认业务工作空间。
// 已有工作空间和其中的数据保持原样；生产环境通过关闭 KBOT_AUTOSEED_ADMIN 跳过首启数据。
func (s *Service) EnsureSeedWorkspaces(ctx context.Context) error {
	existing, err := s.store.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		return fmt.Errorf("list seed workspaces: %w", err)
	}
	byName := make(map[string]struct{}, len(existing))
	for _, workspace := range existing {
		byName[workspace.Name] = struct{}{}
	}
	for _, seed := range DefaultSeedWorkspaces {
		if _, ok := byName[seed.Name]; ok {
			continue
		}
		if _, err := s.CreateWorkspace(ctx, seed.Name, seed.Description, nil); err != nil {
			return fmt.Errorf("ensure seed workspace %q: %w", seed.Name, err)
		}
		byName[seed.Name] = struct{}{}
	}
	return nil
}

// EnsureSeedWorkspaceOwners 让课堂 seed 管理员在成员列表中拥有明确 owner 身份。
func (s *Service) EnsureSeedWorkspaceOwners(ctx context.Context, email string) error {
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return err
	}
	workspaces, err := s.store.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		return err
	}
	seedNames := make(map[string]struct{}, len(DefaultSeedWorkspaces))
	for _, seed := range DefaultSeedWorkspaces {
		seedNames[seed.Name] = struct{}{}
	}
	for _, workspace := range workspaces {
		if _, ok := seedNames[workspace.Name]; !ok {
			continue
		}
		member := &domain.WorkspaceMember{
			WorkspaceID: workspace.ID, UserID: user.ID, Role: WorkspaceRoleOwner, CreatedAt: time.Now(),
		}
		if err := s.store.UpsertWorkspaceMember(ctx, member); err != nil {
			return fmt.Errorf("assign seed workspace owner: %w", err)
		}
	}
	return nil
}

// ListUsers 列出用户。limit<=0 时默认 50。
func (s *Service) ListUsers(ctx context.Context, limit, offset int32) ([]*domain.User, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListUsers(ctx, limit, offset)
}

// ListWorkspaces 列出工作空间。limit<=0 时默认 50。
func (s *Service) ListWorkspaces(ctx context.Context, limit, offset int32) ([]*domain.Workspace, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListWorkspaces(ctx, limit, offset)
}

// ListAccessibleWorkspaces 只返回用户可访问的工作空间；全局管理员可以查看全部。
func (s *Service) ListAccessibleWorkspaces(ctx context.Context, userID string, limit, offset int32) ([]*domain.Workspace, error) {
	if limit <= 0 {
		limit = 50
	}
	admin, err := s.IsGlobalAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	if admin {
		return s.store.ListWorkspaces(ctx, limit, offset)
	}
	return s.store.ListUserWorkspaces(ctx, userID, limit, offset)
}

// IsGlobalAdmin 判断用户是否拥有平台管理员角色，并同时校验账号仍处于 active 状态。
func (s *Service) IsGlobalAdmin(ctx context.Context, userID string) (bool, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return user.Status == "active" && user.Role == GlobalRoleAdmin, nil
}

// GetUser 返回当前持久化用户，用于请求期账号状态与角色校验。
func (s *Service) GetUser(ctx context.Context, userID string) (*domain.User, error) {
	return s.store.GetUserByID(ctx, userID)
}

// WorkspaceRole 返回用户的有效工作空间角色；全局管理员直接获得 admin 权限。
func (s *Service) WorkspaceRole(ctx context.Context, userID, workspaceID string) (string, error) {
	if workspaceID == "" || userID == "" {
		return "", ErrForbidden
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if user.Status != "active" {
		return "", ErrForbidden
	}
	if user.Role == GlobalRoleAdmin {
		return WorkspaceRoleAdmin, nil
	}
	member, err := s.store.GetWorkspaceMember(ctx, workspaceID, userID)
	if err != nil {
		return "", ErrForbidden
	}
	return member.Role, nil
}

// workspaceManagementAuthority 同时判断成员管理权限和 owner 角色管理权限。
func (s *Service) workspaceManagementAuthority(ctx context.Context, actorID, workspaceID string) (bool, error) {
	if actorID == "" || workspaceID == "" {
		return false, ErrForbidden
	}
	user, err := s.store.GetUserByID(ctx, actorID)
	if err != nil || user.Status != "active" {
		return false, ErrForbidden
	}
	if user.Role == GlobalRoleAdmin {
		return true, nil
	}
	member, err := s.store.GetWorkspaceMember(ctx, workspaceID, actorID)
	if err != nil {
		return false, ErrForbidden
	}
	switch member.Role {
	case WorkspaceRoleOwner:
		return true, nil
	case WorkspaceRoleAdmin:
		return false, nil
	default:
		return false, ErrForbidden
	}
}

// CanManageWorkspace 允许全局管理员及工作空间 owner/admin 管理成员。
func (s *Service) CanManageWorkspace(ctx context.Context, actorID, workspaceID string) (bool, error) {
	role, err := s.WorkspaceRole(ctx, actorID, workspaceID)
	if err != nil {
		return false, err
	}
	return role == WorkspaceRoleOwner || role == WorkspaceRoleAdmin, nil
}

func validWorkspaceRole(role string) bool {
	switch role {
	case WorkspaceRoleOwner, WorkspaceRoleAdmin, WorkspaceRoleEditor, WorkspaceRoleMember, WorkspaceRoleViewer:
		return true
	default:
		return false
	}
}

// ListWorkspaceMembers 列出成员，调用方必须拥有成员管理权限。
func (s *Service) ListWorkspaceMembers(ctx context.Context, actorID, workspaceID string) ([]*domain.WorkspaceMember, error) {
	ok, err := s.CanManageWorkspace(ctx, actorID, workspaceID)
	if err != nil || !ok {
		return nil, ErrForbidden
	}
	return s.store.ListWorkspaceMembers(ctx, workspaceID)
}

// UpsertWorkspaceMember 新增成员或调整角色。
func (s *Service) UpsertWorkspaceMember(ctx context.Context, actorID, workspaceID, userID, role string) (*domain.WorkspaceMember, error) {
	manageOwners, err := s.workspaceManagementAuthority(ctx, actorID, workspaceID)
	if err != nil {
		return nil, ErrForbidden
	}
	if !validWorkspaceRole(role) {
		return nil, fmt.Errorf("invalid workspace role %q", role)
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return nil, fmt.Errorf("get member user: %w", err)
	}
	member := &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: role, CreatedAt: time.Now()}
	if guarded, ok := s.store.(guardedMembershipStore); ok {
		if err := guarded.UpsertWorkspaceMemberGuarded(ctx, member, manageOwners); err != nil {
			return nil, err
		}
	} else if err := s.upsertWorkspaceMemberFallback(ctx, member, manageOwners); err != nil {
		return nil, fmt.Errorf("upsert workspace member: %w", err)
	}
	return s.store.GetWorkspaceMember(ctx, workspaceID, userID)
}

func (s *Service) upsertWorkspaceMemberFallback(ctx context.Context, member *domain.WorkspaceMember, manageOwners bool) error {
	existing, existingErr := s.store.GetWorkspaceMember(ctx, member.WorkspaceID, member.UserID)
	if member.Role == WorkspaceRoleOwner && !manageOwners {
		return ErrForbidden
	}
	if existingErr == nil && existing.Role == WorkspaceRoleOwner {
		if !manageOwners {
			return ErrForbidden
		}
		if member.Role != WorkspaceRoleOwner {
			members, err := s.store.ListWorkspaceMembers(ctx, member.WorkspaceID)
			if err != nil {
				return err
			}
			owners := 0
			for _, candidate := range members {
				if candidate.Role == WorkspaceRoleOwner {
					owners++
				}
			}
			if owners <= 1 {
				return ErrLastOwner
			}
		}
	}
	return s.store.UpsertWorkspaceMember(ctx, member)
}

// DeleteWorkspaceMember 移除成员。最后一位 owner 不能被删除。
func (s *Service) DeleteWorkspaceMember(ctx context.Context, actorID, workspaceID, userID string) error {
	manageOwners, err := s.workspaceManagementAuthority(ctx, actorID, workspaceID)
	if err != nil {
		return ErrForbidden
	}
	if guarded, ok := s.store.(guardedMembershipStore); ok {
		deleted, err := guarded.DeleteWorkspaceMemberGuarded(ctx, workspaceID, userID, manageOwners)
		if err != nil {
			return err
		}
		if !deleted {
			return fmt.Errorf("workspace member not found")
		}
		return nil
	}
	members, err := s.store.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return err
	}
	owners := 0
	targetOwner := false
	for _, member := range members {
		if member.Role == WorkspaceRoleOwner {
			owners++
			if member.UserID == userID {
				targetOwner = true
			}
		}
	}
	if targetOwner && !manageOwners {
		return ErrForbidden
	}
	if targetOwner && owners <= 1 {
		return ErrLastOwner
	}
	deleted, err := s.store.DeleteWorkspaceMember(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("workspace member not found")
	}
	return nil
}

// CreateWorkspace 创建工作空间
func (s *Service) CreateWorkspace(ctx context.Context, name, description string, parentID *string) (*domain.Workspace, error) {
	workspace := &domain.Workspace{
		ID:          util.GenerateID(),
		Name:        name,
		Description: description,
		ParentID:    parentID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.store.CreateWorkspace(ctx, workspace); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	return workspace, nil
}

// CreateWorkspaceForUser 创建工作空间并把创建者设为 owner。
func (s *Service) CreateWorkspaceForUser(ctx context.Context, actorID, name, description string, parentID *string) (*domain.Workspace, error) {
	workspace := &domain.Workspace{
		ID: util.GenerateID(), Name: name, Description: description, ParentID: parentID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if atomicStore, ok := s.store.(workspaceOwnerStore); ok {
		if err := atomicStore.CreateWorkspaceWithOwner(ctx, workspace, actorID); err != nil {
			return nil, fmt.Errorf("create workspace with owner: %w", err)
		}
		return workspace, nil
	}
	if err := s.store.CreateWorkspace(ctx, workspace); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	member := &domain.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actorID, Role: WorkspaceRoleOwner, CreatedAt: time.Now()}
	if err := s.store.UpsertWorkspaceMember(ctx, member); err != nil {
		return nil, fmt.Errorf("assign workspace owner: %w", err)
	}
	return workspace, nil
}
