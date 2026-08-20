package iam

import (
	"context"
	"errors"

	"github.com/Q1mi/kbot/internal/domain"
)

var ErrNotImplemented = errors.New("IAM service is not implemented")

type Store interface {
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
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

func (s *Service) Register(context.Context, string, string, string) (*domain.User, error) {
	return nil, ErrNotImplemented
}

func (s *Service) Login(context.Context, string, string) (*LoginResult, error) {
	return nil, ErrNotImplemented
}

func (s *Service) ParseToken(string) (string, error) {
	return "", ErrNotImplemented
}
