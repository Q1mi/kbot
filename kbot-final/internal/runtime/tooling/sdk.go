package tooling

import (
	"context"
	"encoding/json"
)

// ExecutorFunc 让普通函数满足 Executor 接口，方便注册 internal_sdk 工具。
type ExecutorFunc func(ctx context.Context, args json.RawMessage) (string, error)

func (f ExecutorFunc) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return f(ctx, args)
}
