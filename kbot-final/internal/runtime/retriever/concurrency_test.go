package retriever

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestSearchIndexNoRace 守护「边 ingest 边检索」的并发安全。
//
// 这是 RAG 的必然并发场景:一个 goroutine 持续 Index(写锁,改 idx.chunks/df/totalLen),
// 另一些 goroutine 同时 Search(读 idx)。修复前 Search 在释放读锁后才读 idx 字段,
// 与 Index 形成数据竞争(尤其 idx.df 的并发读写会 fatal),`go test -race` 必报。
// 必须用 -race 跑才有意义。
func TestSearchIndexNoRace(t *testing.T) {
	r := New(NewLocalEmbedder(64))
	ctx := context.Background()

	// 预置一点内容,保证 Search 命中非空索引分支。
	seed := func(docID, content string) Chunk {
		embs, _ := r.embedder.Embed(ctx, []string{content})
		return Chunk{ID: docID, DocID: docID, Content: content, Embedding: embs[0]}
	}
	if err := r.Index(ctx, "kb1", []Chunk{seed("d0", "退款政策与订单号")}); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	var wg sync.WaitGroup

	// 写方:不断 index 新文档(触发 removeDocs + append + df 变更)。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			docID := fmt.Sprintf("d%d", i)
			_ = r.Index(ctx, "kb1", []Chunk{seed(docID, fmt.Sprintf("文档 %d 退款 发票 物流", i))})
		}
	}()

	// 读方:并发检索。
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if _, err := r.Search(ctx, "kb1", "退款 发票", 5); err != nil {
					t.Errorf("search: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
}
