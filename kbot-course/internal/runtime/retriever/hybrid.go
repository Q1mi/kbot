// Package retriever 负责 Agent 的知识检索。
package retriever

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	type response struct {
		results []Result
		err     error
	}
	keywordCh, vectorCh := make(chan response, 1), make(chan response, 1)
	go func() {
		results, err := h.keyword.Search(ctx, workspaceID, query, limit*2)
		keywordCh <- response{results, err}
	}()
	go func() {
		results, err := h.vector.Search(ctx, workspaceID, query, limit*2)
		vectorCh <- response{results, err}
	}()
	keyword, vector := <-keywordCh, <-vectorCh
	if keyword.err != nil {
		return nil, fmt.Errorf("keyword search: %w", keyword.err)
	}
	if vector.err != nil {
		return nil, fmt.Errorf("vector search: %w", vector.err)
	}
	const rrfK = 60.0
	merged := make(map[string]Result)
	add := func(results []Result) {
		seen := make(map[string]struct{}, len(results))
		for rank, result := range results {
			if _, duplicate := seen[result.ID]; duplicate {
				continue
			}
			seen[result.ID] = struct{}{}
			current := merged[result.ID]
			if current.ID == "" {
				current = Result{ID: result.ID, SourceURI: result.SourceURI, Text: result.Text}
			}
			current.Score += 1 / (rrfK + float64(rank+1))
			merged[result.ID] = current
		}
	}
	add(keyword.results)
	add(vector.results)
	out := make([]Result, 0, len(merged))
	for _, result := range merged {
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
