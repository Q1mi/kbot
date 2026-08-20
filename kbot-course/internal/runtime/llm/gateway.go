// Package llm 把模型 SDK 收敛为 Runtime 使用的稳定接口。
package llm

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/schema"
)

var ErrNotImplemented = errors.New("LLM gateway is not implemented")

// Gateway 将在 end 版本接入 Eino 的 OpenAI 兼容模型。
type Gateway struct{}

func NewGateway() *Gateway { return &Gateway{} }

func (g *Gateway) Generate(
	context.Context,
	[]*schema.Message,
	[]*schema.ToolInfo,
) (*schema.Message, error) {
	return nil, ErrNotImplemented
}
