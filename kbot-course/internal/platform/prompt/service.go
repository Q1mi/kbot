// Package prompt 管理可审计、可固定引用的 Prompt 版本。
package prompt

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("prompt versions are implemented in 12-end")

type Version struct {
	ID          string
	WorkspaceID string
	Name        string
	Template    string
}

type Service struct{}

func NewService() *Service                                { return &Service{} }
func (s *Service) Publish(context.Context, Version) error { return ErrNotImplemented }
func (s *Service) Render(context.Context, string, string, map[string]string) (string, error) {
	return "", ErrNotImplemented
}
