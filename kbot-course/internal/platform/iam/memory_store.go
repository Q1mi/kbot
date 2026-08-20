package iam

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Q1mi/kbot/internal/domain"
)

var ErrUserNotFound = errors.New("user not found")
var ErrMembershipNotFound = errors.New("workspace membership not found")

type MemoryStore struct {
	mu          sync.RWMutex
	byID        map[string]*domain.User
	byEmail     map[string]string
	workspaces  map[string]*domain.Workspace
	memberships map[string]map[string]domain.Membership
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:        make(map[string]*domain.User),
		byEmail:     make(map[string]string),
		workspaces:  make(map[string]*domain.Workspace),
		memberships: make(map[string]map[string]domain.Membership),
	}
}

func (s *MemoryStore) CreateWorkspace(_ context.Context, workspace *domain.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.workspaces[workspace.ID]; exists {
		return errors.New("workspace already exists")
	}
	clone := *workspace
	s.workspaces[workspace.ID] = &clone
	return nil
}

func (s *MemoryStore) AddMembership(_ context.Context, membership *domain.Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID[membership.UserID] == nil || s.workspaces[membership.WorkspaceID] == nil {
		return errors.New("membership references an unknown user or workspace")
	}
	if s.memberships[membership.UserID] == nil {
		s.memberships[membership.UserID] = make(map[string]domain.Membership)
	}
	s.memberships[membership.UserID][membership.WorkspaceID] = *membership
	return nil
}

func (s *MemoryStore) HasMembership(_ context.Context, userID, workspaceID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.memberships[userID][workspaceID]
	return ok, nil
}

func (s *MemoryStore) GetMembership(_ context.Context, userID, workspaceID string) (*domain.Membership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	membership, ok := s.memberships[userID][workspaceID]
	if !ok {
		return nil, ErrMembershipNotFound
	}
	clone := membership
	return &clone, nil
}

func (s *MemoryStore) ListUserWorkspaces(_ context.Context, userID string) ([]*domain.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*domain.Workspace, 0, len(s.memberships[userID]))
	for workspaceID := range s.memberships[userID] {
		workspace := s.workspaces[workspaceID]
		if workspace == nil {
			continue
		}
		clone := *workspace
		result = append(result, &clone)
	}
	return result, nil
}

func (s *MemoryStore) CreateUser(_ context.Context, user *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(user.Email)
	if _, exists := s.byEmail[key]; exists {
		return errors.New("email already exists")
	}
	clone := *user
	s.byID[user.ID] = &clone
	s.byEmail[key] = user.ID
	return nil
}

func (s *MemoryStore) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byEmail[strings.ToLower(email)]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *s.byID[id]
	return &clone, nil
}

func (s *MemoryStore) GetUser(_ context.Context, id string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *user
	return &clone, nil
}
