// Package connector 定义知识源接入协议。
package connector

import "context"

type Document struct {
	SourceURI string
	Title     string
	Content   string
	Checksum  string
}

type Connector interface {
	Scan(ctx context.Context) ([]Document, error)
}
