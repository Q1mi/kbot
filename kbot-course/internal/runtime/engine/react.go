package engine

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/schema"
)

var ErrADKRuntimeNotImplemented = errors.New("Eino ADK runtime is implemented in 09-end")

type ToolBinding struct {
	Name      string
	VersionID string
	Info      *schema.ToolInfo
}

// ADKRunner 将平台 Tool Binding 装配到 ChatModelAgent、ToolsNode 与 Runner。
type ADKRunner struct{}

func NewADKRunner() *ADKRunner { return &ADKRunner{} }

func (r *ADKRunner) Run(context.Context, []*schema.Message, []ToolBinding, int, Emitter) (*schema.Message, error) {
	return nil, ErrADKRuntimeNotImplemented
}
