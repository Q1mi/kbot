// Package approval 管理高风险工具调用的人工审批与运行检查点。
package approval

import (
	"context"
	"errors"
	"time"
)

var ErrNotImplemented = errors.New("approval checkpoint is implemented in 16-end")

type Request struct {
	ID, WorkspaceID, RunID, ToolCallID, ToolVersionID string
	Arguments                                         []byte
	Checkpoint                                        []byte
	Status                                            string
	ExpiresAt                                         time.Time
}

type Service struct{}

func NewService() *Service                                            { return &Service{} }
func (s *Service) Create(context.Context, Request) (*Request, error)  { return nil, ErrNotImplemented }
func (s *Service) Decide(context.Context, string, string, bool) error { return ErrNotImplemented }
func (s *Service) Resume(context.Context, string, string, []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}
