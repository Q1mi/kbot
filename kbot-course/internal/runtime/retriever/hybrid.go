// Package retriever 负责 Agent 的知识检索。
package retriever

import (
	"context"
	"fmt"
	"strings"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/flow/retriever/router"
	"github.com/cloudwego/eino/schema"
)

type Result struct {
	ID        string
	SourceURI string
	Text      string
	Score     float64
}

type Searcher interface {
	Search(ctx context.Context, workspaceID, query string, limit int) ([]Result, error)
}

type Hybrid struct {
	keyword Searcher
	vector  Searcher
}

func NewHybrid(keyword, vector Searcher) *Hybrid { return &Hybrid{keyword: keyword, vector: vector} }

func (h *Hybrid) Search(ctx context.Context, workspaceID, query string, limit int) ([]Result, error) {
	if h.keyword == nil || h.vector == nil {
		return nil, fmt.Errorf("keyword and vector searchers are required")
	}
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, fmt.Errorf("workspace, query and positive limit are required")
	}
	hybrid, err := router.NewRetriever(ctx, &router.Config{
		Retrievers: map[string]einoretriever.Retriever{
			"keyword": &searcherRetriever{searcher: h.keyword, workspaceID: workspaceID},
			"vector":  &searcherRetriever{searcher: h.vector, workspaceID: workspaceID},
		},
		Router: func(context.Context, string) ([]string, error) {
			return []string{"keyword", "vector"}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create Eino retriever router: %w", err)
	}
	documents, err := hybrid.Retrieve(ctx, query, einoretriever.WithTopK(limit*2))
	if err != nil {
		return nil, fmt.Errorf("hybrid retrieve: %w", err)
	}
	out := make([]Result, 0, len(documents))
	for _, document := range documents {
		sourceURI, _ := document.MetaData["source_uri"].(string)
		out = append(out, Result{ID: document.ID, SourceURI: sourceURI, Text: document.Content})
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type searcherRetriever struct {
	searcher    Searcher
	workspaceID string
}

func (r *searcherRetriever) Retrieve(
	ctx context.Context, query string, opts ...einoretriever.Option,
) ([]*schema.Document, error) {
	topK := 10
	if configured := einoretriever.GetCommonOptions(nil, opts...).TopK; configured != nil && *configured > 0 {
		topK = *configured
	}
	results, err := r.searcher.Search(ctx, r.workspaceID, query, topK)
	if err != nil {
		return nil, err
	}
	documents := make([]*schema.Document, 0, len(results))
	for _, result := range results {
		documents = append(documents, &schema.Document{
			ID: result.ID, Content: result.Text,
			MetaData: map[string]any{"source_uri": result.SourceURI, "raw_score": result.Score},
		})
	}
	return documents, nil
}
