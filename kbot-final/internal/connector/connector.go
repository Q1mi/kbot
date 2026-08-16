// Package connector 定义 KB 数据源接入的统一抽象（设计文档 §4.5 / 讲义 §14.3）。
//
// 本平台只实现一个 reference connector（markdown_folder）。接 Confluence / GitLab /
// 企业网盘等真实数据源，本质都是同样的接口加上 OAuth 与分页——作为扩展练习。
package connector

import (
	"context"
	"io"
	"time"
)

// DocMeta 是一份文档的元信息。Hash 用来在 KB 端判断是否需要重新 ingest。
type DocMeta struct {
	ID        string
	Title     string
	UpdatedAt time.Time
	Hash      string
}

// Connector 是可插拔的数据源接入抽象。
type Connector interface {
	Name() string
	// ListDocuments 返回当前游标之后的批量文档元信息（增量）。
	ListDocuments(ctx context.Context, cursor string) (docs []DocMeta, nextCursor string, err error)
	// FetchDocument 取一份文档的内容。
	FetchDocument(ctx context.Context, docID string) (io.ReadCloser, error)
}
