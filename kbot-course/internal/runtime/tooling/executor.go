// Package tooling 负责 Runtime 侧的工具解析、校验与调用。
package tooling

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("tool executor is implemented in 08-end")

type Call struct {
	WorkspaceID    string
	ToolVersionID  string
	Arguments      []byte
	IdempotencyKey string
}

type Result struct {
	StatusCode int
	Body       []byte
}

// SandboxRunner 是 code_execution Tool 依赖的最小执行端口。
// 第 08 课结束时会由独立 HTTP Client 实现。
type SandboxRunner interface {
	Run(ctx context.Context, language, code string) (string, error)
}

type Executor struct {
	sandbox SandboxRunner
}

func NewExecutor() *Executor { return &Executor{} }

func (e *Executor) WithSandbox(runner SandboxRunner) *Executor {
	e.sandbox = runner
	return e
}

func (e *Executor) Execute(context.Context, Call) (Result, error) {
	return Result{}, ErrNotImplemented
}
