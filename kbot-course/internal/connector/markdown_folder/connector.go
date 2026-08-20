// Package markdown_folder 从本地 Markdown 目录读取课堂知识库。
package markdown_folder

import (
	"context"
	"errors"

	"github.com/Q1mi/kbot/internal/connector"
)

var ErrNotImplemented = errors.New("markdown connector is implemented in 10-end")

type Connector struct{ Root string }

func New(root string) *Connector { return &Connector{Root: root} }

func (c *Connector) Scan(context.Context) ([]connector.Document, error) {
	return nil, ErrNotImplemented
}
