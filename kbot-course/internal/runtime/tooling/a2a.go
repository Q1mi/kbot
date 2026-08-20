package tooling

import (
	"context"
	"errors"
)

var ErrA2ANotImplemented = errors.New("A2A client is implemented in 21-end")

type A2AResult struct{ TaskID, Status, Content string }
type A2AClient struct{}

func NewA2AClient(string) *A2AClient { return &A2AClient{} }
func (c *A2AClient) Send(context.Context, string, string) (A2AResult, error) {
	return A2AResult{}, ErrA2ANotImplemented
}
