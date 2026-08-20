// Package eval 提供 Agent 版本发布前的离线回归评测。
package eval

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("evaluation service is implemented in 20-end")

type Case struct {
	ID, Input, ExpectedContains string
	Forbidden                   []string
	ExpectedTools               []string
}
type Output struct {
	Content string
	Tools   []string
}
type Agent interface {
	Run(context.Context, string) (Output, error)
}
type CaseResult struct {
	CaseID  string
	Passed  bool
	Reasons []string
}
type Report struct {
	Results  []CaseResult
	PassRate float64
}
type Service struct{}

func NewService() *Service { return &Service{} }
func (s *Service) Run(context.Context, []Case, Agent) (Report, error) {
	return Report{}, ErrNotImplemented
}
func Gate(Report, float64) error { return ErrNotImplemented }
