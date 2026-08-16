package retriever

import (
	"context"
	"testing"
)

func TestSearchMode_ThreeModes(t *testing.T) {
	r := indexCorpus(t)
	ctx := context.Background()
	q := "怎么申请退款 SKU-2024"

	for _, mode := range []string{ModeBM25, ModeVector, ModeHybrid} {
		got, err := r.SearchMode(ctx, "kb1", q, 3, mode)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if len(got) == 0 {
			t.Fatalf("mode %s: expected results, got none", mode)
		}
		// 三档对该查询都应把退款文档排在前列(语料里只有它含 SKU-2024)。
		if got[0].DocID != "doc-refund" {
			t.Fatalf("mode %s: expected doc-refund first, got %s", mode, got[0].DocID)
		}
	}

	// 未知 mode 回退 hybrid。
	got, err := r.SearchMode(ctx, "kb1", q, 3, "garbage")
	if err != nil || len(got) == 0 {
		t.Fatalf("unknown mode should fall back to hybrid: %v / %d", err, len(got))
	}

	// 空 KB 各档都返回空而非报错。
	for _, mode := range []string{ModeBM25, ModeVector, ModeHybrid} {
		out, err := r.SearchMode(ctx, "nope", q, 3, mode)
		if err != nil {
			t.Fatalf("empty kb mode %s err: %v", mode, err)
		}
		if len(out) != 0 {
			t.Fatalf("empty kb mode %s should be empty, got %d", mode, len(out))
		}
	}
}

// 确认 *Retriever 满足可选接口 ModeSearcher。
func TestRetrieverImplementsModeSearcher(t *testing.T) {
	var _ ModeSearcher = New(NewLocalEmbedder(64))
}
