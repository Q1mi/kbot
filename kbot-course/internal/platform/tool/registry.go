// Package tool 管理可被 Agent 固定引用的工具版本。
package tool

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("tool registry is implemented in 07-end")

type Version struct {
	ID          string
	WorkspaceID string
	Name        string
	Description string
	InputSchema []byte
	Endpoint    string
	Published   bool
}

type Registry struct{}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(context.Context, Version) error { return ErrNotImplemented }

func (r *Registry) Resolve(context.Context, string, string) (Version, error) {
	return Version{}, ErrNotImplemented
}
