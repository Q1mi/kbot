package iam

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Q1mi/kbot/internal/domain"
)

var ErrUserNotFound = errors.New("user not found")

type MemoryStore struct {
	mu      sync.RWMutex
	byID    map[string]*domain.User
	byEmail map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:    make(map[string]*domain.User),
		byEmail: make(map[string]string),
	}
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
