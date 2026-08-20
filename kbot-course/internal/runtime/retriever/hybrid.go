// Package retriever 负责 Agent 的知识检索。
package retriever

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("hybrid retrieval is implemented in 11-end")

type Result struct {
	ID        string
	SourceURI string
	Text      string
	Score     float64
}

type Searcher interface {
	Search(ctx context.Context, workspaceID, query string, limit int) ([]Result, error)
}

type Hybrid struct{}

func NewHybrid(Searcher, Searcher) *Hybrid { return &Hybrid{} }

func (h *Hybrid) Search(context.Context, string, string, int) ([]Result, error) {
	return nil, ErrNotImplemented
}
