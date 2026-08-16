package retriever

import (
	"context"
	"testing"
)

// indexCorpus 是测试用的小语料。
func indexCorpus(t *testing.T) *Retriever {
	t.Helper()
	r := New(NewLocalEmbedder(256))
	docs := []struct {
		id, content string
	}{
		{"doc-refund", "退款政策：用户在七天内可以申请退款，需要提供订单号 SKU-2024。"},
		{"doc-shipping", "物流配送说明：标准快递三到五个工作日送达，偏远地区顺延。"},
		{"doc-invoice", "发票开具：企业用户可在订单完成后申请增值税专用发票。"},
	}
	chunks := make([]Chunk, 0, len(docs))
	for i, d := range docs {
		embs, err := r.embedder.Embed(context.Background(), []string{d.content})
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		chunks = append(chunks, Chunk{
			ID:        d.id,
			DocID:     d.id,
			Ordinal:   i,
			Content:   d.content,
			Embedding: embs[0],
		})
	}
	if err := r.Index(context.Background(), "kb1", chunks); err != nil {
		t.Fatalf("index: %v", err)
	}
	return r
}

func TestHybridSearchRanksRelevantDocFirst(t *testing.T) {
	r := indexCorpus(t)

	got, err := r.Search(context.Background(), "kb1", "怎么申请退款 SKU-2024", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected results, got none")
	}
	if got[0].DocID != "doc-refund" {
		t.Fatalf("expected doc-refund first, got %s (results: %+v)", got[0].DocID, got)
	}
}

func TestSearchEmptyKB(t *testing.T) {
	r := New(NewLocalEmbedder(64))
	got, err := r.Search(context.Background(), "nope", "anything", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty kb, got %+v", got)
	}
}

func TestBM25KeywordMatch(t *testing.T) {
	r := indexCorpus(t)
	r.mu.RLock()
	idx := r.indexes["kb1"]
	r.mu.RUnlock()

	// "发票" 这个关键词只在 doc-invoice 出现，BM25 应把它排第一。
	got := bm25Search(idx, "增值税专用发票", 3)
	if len(got) == 0 {
		t.Fatal("expected bm25 hits")
	}
	if got[0].DocID != "doc-invoice" {
		t.Fatalf("expected doc-invoice first, got %s", got[0].DocID)
	}
}

func TestRRFMergesAndDedups(t *testing.T) {
	a := []Passage{{ChunkID: "x", DocID: "x"}, {ChunkID: "y", DocID: "y"}}
	b := []Passage{{ChunkID: "y", DocID: "y"}, {ChunkID: "z", DocID: "z"}}

	merged := rrf(a, b, 60)
	if len(merged) != 3 {
		t.Fatalf("expected 3 unique results, got %d", len(merged))
	}
	// y 在两路都靠前，应排第一。
	if merged[0].ChunkID != "y" {
		t.Fatalf("expected y first (appears in both lists), got %s", merged[0].ChunkID)
	}
}

func TestIndexIdempotentOnReindex(t *testing.T) {
	r := indexCorpus(t)

	// 重新索引同一文档（内容变更），不应产生重复 chunk。
	embs, _ := r.embedder.Embed(context.Background(), []string{"退款政策已更新：现在支持十四天无理由退款。"})
	err := r.Index(context.Background(), "kb1", []Chunk{{
		ID: "doc-refund", DocID: "doc-refund", Content: "退款政策已更新：现在支持十四天无理由退款。", Embedding: embs[0],
	}})
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}

	r.mu.RLock()
	idx := r.indexes["kb1"]
	count := 0
	for _, c := range idx.chunks {
		if c.DocID == "doc-refund" {
			count++
		}
	}
	r.mu.RUnlock()

	if count != 1 {
		t.Fatalf("expected exactly 1 chunk for doc-refund after reindex, got %d", count)
	}
}
