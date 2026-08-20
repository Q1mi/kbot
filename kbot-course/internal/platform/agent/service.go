// Package agent 管理 Agent 版本、环境发布和会话固定。
package agent

import (
	"context"
	"errors"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

var ErrNotImplemented = errors.New("agent snapshot pinning is implemented in 14-end")

type Service struct{}

func NewService() *Service { return &Service{} }
func (s *Service) Publish(context.Context, domain.AgentVersion, engine.AgentSnapshot) error {
	return ErrNotImplemented
}
func (s *Service) Promote(context.Context, string, string, string, string) error {
	return ErrNotImplemented
}
func (s *Service) CreateConversation(context.Context, string, string, string, string) (*domain.Conversation, error) {
	return nil, ErrNotImplemented
}
func (s *Service) Snapshot(context.Context, string, string) (*engine.AgentSnapshot, error) {
	return nil, ErrNotImplemented
}
