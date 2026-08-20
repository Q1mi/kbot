package retriever

import (
	"context"
	"fmt"
	"testing"

	"github.com/Q1mi/kbot/internal/platform/kb"
)

type documentSourceFunc func(context.Context, string, string) ([]kb.Document, error)

func (f documentSourceFunc) Documents(ctx context.Context, workspaceID, kbID string) ([]kb.Document, error) {
	return f(ctx, workspaceID, kbID)
}

type mapEmbedder map[string][]float64

func (e mapEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	vector, ok := e[text]
	if !ok {
		return nil, fmt.Errorf("missing vector for %q", text)
	}
	return append([]float64(nil), vector...), nil
}

func TestKnowledgeSearchRunsAllRetrievalModes(t *testing.T) {
	source := documentSourceFunc(func(_ context.Context, workspaceID, kbID string) ([]kb.Document, error) {
		if workspaceID != "ws-1" || kbID != "kb-1" {
			t.Fatalf("scope = %s/%s", workspaceID, kbID)
		}
		return []kb.Document{
			{ID: "doc-1", SourceURI: "fulfillment.md", Chunks: []string{"深圳仓无库存时，检查洛杉矶仓并创建库存调拨。"}},
			{ID: "doc-2", SourceURI: "refund.md", Chunks: []string{"退款需要核验订单状态。"}},
		}, nil
	})
	search := NewKnowledgeSearch(source)
	for _, mode := range []string{"bm25", "vector", "hybrid"} {
		results, err := search.Search(t.Context(), "ws-1", "kb-1", "库存调拨", mode, 2)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if len(results) == 0 || results[0].SourceURI != "fulfillment.md" {
			t.Fatalf("mode %s results = %#v", mode, results)
		}
	}
}

func TestBM25UsesCorpusFrequencyAndLengthNormalization(t *testing.T) {
	source := documentSourceFunc(func(context.Context, string, string) ([]kb.Document, error) {
		return []kb.Document{
			{ID: "short", SourceURI: "short.md", Chunks: []string{"refund policy"}},
			{ID: "long", SourceURI: "long.md", Chunks: []string{"refund policy with many unrelated shipping inventory warehouse payment words"}},
			{ID: "other", SourceURI: "other.md", Chunks: []string{"inventory transfer"}},
		}, nil
	})
	results, err := NewKnowledgeSearch(source).Search(t.Context(), "ws", "kb", "refund", "bm25", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].ID != "short:0" || results[0].Score <= results[1].Score {
		t.Fatalf("BM25 results = %#v", results)
	}
}

func TestVectorModeUsesInjectedEmbeddingProvider(t *testing.T) {
	source := documentSourceFunc(func(context.Context, string, string) ([]kb.Document, error) {
		return []kb.Document{
			{ID: "semantic", SourceURI: "semantic.md", Chunks: []string{"warehouse relocation"}},
			{ID: "lexical", SourceURI: "lexical.md", Chunks: []string{"库存调拨"}},
		}, nil
	})
	embedder := mapEmbedder{
		"warehouse relocation": {1, 0},
		"库存调拨":                 {0, 1},
		"如何移动仓库库存":             {1, 0},
	}
	results, err := NewKnowledgeSearch(source, embedder).Search(t.Context(), "ws", "kb", "如何移动仓库库存", "vector", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != "semantic:0" {
		t.Fatalf("vector results = %#v", results)
	}
}
