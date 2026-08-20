package iam

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/Q1mi/kbot/internal/domain"
)

var ErrInvalidCredentials = errors.New("invalid email or password")
var ErrWorkspaceForbidden = errors.New("workspace access denied")

const (
	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleEditor = "editor"
	WorkspaceRoleMember = "member"
	WorkspaceRoleViewer = "viewer"
)

type Store interface {
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
	CreateWorkspace(ctx context.Context, workspace *domain.Workspace) error
	AddMembership(ctx context.Context, membership *domain.Membership) error
	HasMembership(ctx context.Context, userID, workspaceID string) (bool, error)
	GetMembership(ctx context.Context, userID, workspaceID string) (*domain.Membership, error)
	ListUserWorkspaces(ctx context.Context, userID string) ([]*domain.Workspace, error)
}

type Service struct {
	store  Store
	secret []byte
	issuer string
}

func New(store Store, secret, issuer string) *Service {
	return &Service{store: store, secret: []byte(secret), issuer: issuer}
}

type LoginResult struct {
	Token string       `json:"token"`
	User  *domain.User `json:"user"`
}

func (s *Service) Register(ctx context.Context, email, password, name string) (*domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return nil, fmt.Errorf("invalid email")
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must contain at least 8 characters")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user := &domain.User{
		ID: newID(), Email: email, PasswordHash: string(hash),
		Name: name, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	workspace := &domain.Workspace{
		ID: newID(), Name: name + " Workspace", CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateWorkspace(ctx, workspace); err != nil {
		return nil, fmt.Errorf("create default workspace: %w", err)
	}
	if err := s.store.AddMembership(ctx, &domain.Membership{
		UserID: user.ID, WorkspaceID: workspace.ID, Role: WorkspaceRoleOwner, CreatedAt: time.Now().UTC(),
	}); err != nil {
		return nil, fmt.Errorf("create default workspace membership: %w", err)
	}
	return user, nil
}

func (s *Service) CheckWorkspaceAccess(ctx context.Context, userID, workspaceID string) error {
	_, err := s.WorkspaceRole(ctx, userID, workspaceID)
	return err
}

// WorkspaceRole 从服务端成员关系解析请求期角色。客户端只能选择 Workspace，不能声明角色。
func (s *Service) WorkspaceRole(ctx context.Context, userID, workspaceID string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(workspaceID) == "" {
		return "", ErrWorkspaceForbidden
	}
	membership, err := s.store.GetMembership(ctx, userID, workspaceID)
	if err != nil || !validWorkspaceRole(membership.Role) {
		return "", ErrWorkspaceForbidden
	}
	return membership.Role, nil
}

func validWorkspaceRole(role string) bool {
	switch role {
	case WorkspaceRoleOwner, WorkspaceRoleAdmin, WorkspaceRoleEditor, WorkspaceRoleMember, WorkspaceRoleViewer:
		return true
	default:
		return false
	}
}

func (s *Service) ListUserWorkspaces(ctx context.Context, userID string) ([]*domain.Workspace, error) {
	return s.store.ListUserWorkspaces(ctx, userID)
}

func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	user, err := s.store.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	now := time.Now()
	claims := claims{
		UserID: user.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: s.issuer, Subject: user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(8 * time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}
	return &LoginResult{Token: token, User: user}, nil
}

func (s *Service) ParseToken(raw string) (string, error) {
	parsed := &claims{}
	token, err := jwt.ParseWithClaims(raw, parsed, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %s", token.Method.Alg())
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid || parsed.UserID == "" {
		return "", ErrInvalidCredentials
	}
	return parsed.UserID, nil
}

type claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw[:])
}
