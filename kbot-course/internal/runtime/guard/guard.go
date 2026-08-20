// Package guard 在模型调用前后执行可组合安全规则。
package guard

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("guard pipeline is implemented in 18-end")

type Decision struct {
	Allowed        bool
	SanitizedText  string
	Classification string
	Reasons        []string
}

type Rule interface {
	Evaluate(context.Context, string) (Decision, error)
}
type Pipeline struct{}

func NewPipeline(...Rule) *Pipeline { return &Pipeline{} }
func (p *Pipeline) Evaluate(context.Context, string) (Decision, error) {
	return Decision{}, ErrNotImplemented
}
