package tooling

import (
	"context"
	"encoding/json"
	"fmt"
)

// sandboxExecutor 把 code_run_python / code_run_bash 这类 code_execution 工具
// 的入参 {"code": "..."} 交给 SandboxRunner 在隔离容器里执行（§4.3 / §14.2）。
type sandboxExecutor struct {
	sandbox  SandboxRunner
	language string // python / bash
}

func newSandboxExecutor(sandbox SandboxRunner, language string) Executor {
	return &sandboxExecutor{sandbox: sandbox, language: language}
}

type codeArgs struct {
	Code string `json:"code"`
}

func (e *sandboxExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in codeArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("parse code args: %w", err)
	}
	if in.Code == "" {
		return "", fmt.Errorf("empty code")
	}
	if detailed, ok := e.sandbox.(DetailedSandboxRunner); ok {
		result, runErr := detailed.RunDetailed(ctx, e.language, in.Code)
		if observeErr := notifySandboxObserver(ctx, result, runErr); observeErr != nil {
			if runErr != nil {
				return result.Stdout, fmt.Errorf("%v; persist sandbox audit: %w", runErr, observeErr)
			}
			return result.Stdout, fmt.Errorf("persist sandbox audit: %w", observeErr)
		}
		return result.Stdout, runErr
	}
	return e.sandbox.Run(ctx, e.language, in.Code)
}
