// Package modelconfig 管理模型部署配置版本。
package modelconfig

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("model profiles are implemented in 12-end")

type Deployment struct{ Provider, Model, BaseURL string }
type ProfileVersion struct {
	ID, WorkspaceID, Name string
	ClassificationMax     string
	Deployments           []Deployment
}
type Registry struct{}

func NewRegistry() *Registry                                      { return &Registry{} }
func (r *Registry) Publish(context.Context, ProfileVersion) error { return ErrNotImplemented }
func (r *Registry) Resolve(context.Context, string, string) (ProfileVersion, error) {
	return ProfileVersion{}, ErrNotImplemented
}
