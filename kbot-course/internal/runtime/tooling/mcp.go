package tooling

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrMCPNotImplemented = errors.New("MCP client is implemented in 21-end")

type MCPTransport interface {
	Request(context.Context, string, any) (json.RawMessage, error)
}
type MCPClient struct{}

func NewMCPClient(MCPTransport) *MCPClient { return &MCPClient{} }
func (c *MCPClient) CallTool(context.Context, string, map[string]any) (json.RawMessage, error) {
	return nil, ErrMCPNotImplemented
}
