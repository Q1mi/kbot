// Package team 通过固定 Agent 版本组织多 Agent 协作。
package team

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("team coordinator is implemented in 21-end")

type Member struct{ Name, AgentVersionID string }
type Worker interface {
	Run(context.Context, string, string) (string, error)
}
type Coordinator struct{}

func NewCoordinator([]Member, Worker) (*Coordinator, error) { return nil, ErrNotImplemented }
func (c *Coordinator) Dispatch(context.Context, string, string) (string, error) {
	return "", ErrNotImplemented
}
