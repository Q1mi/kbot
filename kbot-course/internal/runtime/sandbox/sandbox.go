// Package sandbox 提供独立代码执行服务的契约与 Docker 执行后端。
package sandbox

import (
	"context"
	"errors"
	"time"
)

var ErrNotImplemented = errors.New("sandbox runner is implemented in 08-end")

// Config 中的限制只能由 Runner 部署环境设置，执行请求不能覆盖。
type Config struct {
	PythonImage    string
	BashImage      string
	NetworkMode    string
	MemoryMB       int
	CPUs           float64
	PidsLimit      int
	Timeout        time.Duration
	TmpfsMB        int
	MaxCodeBytes   int
	MaxOutputBytes int
	MaxConcurrent  int
}

func DefaultConfig() Config {
	return Config{
		PythonImage: "python:3.12-slim", BashImage: "busybox:latest",
		NetworkMode: "none", MemoryMB: 256, CPUs: 0.5, PidsLimit: 64,
		Timeout: 30 * time.Second, TmpfsMB: 64, MaxCodeBytes: 64 << 10,
		MaxOutputBytes: 1 << 20, MaxConcurrent: 4,
	}
}

type Result struct {
	ExecutionID     string `json:"execution_id"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	TimedOut        bool   `json:"timed_out"`
	OutputTruncated bool   `json:"output_truncated"`
}

type Sandbox struct {
	cfg Config
}

func New(cfg Config) *Sandbox { return &Sandbox{cfg: cfg} }

func (s *Sandbox) Available() bool { return false }

func (s *Sandbox) Check(context.Context) error { return ErrNotImplemented }

func (s *Sandbox) Run(ctx context.Context, language, code string) (string, error) {
	result, err := s.Execute(ctx, language, code)
	return result.Stdout, err
}

func (s *Sandbox) Execute(context.Context, string, string) (Result, error) {
	return Result{}, ErrNotImplemented
}
