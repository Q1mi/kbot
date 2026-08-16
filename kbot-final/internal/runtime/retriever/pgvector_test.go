//go:build integration

package retriever

// PgvectorRetriever 集成测试:chunk 落 kb_chunks(vector + tsv 生成列),RRF 检索读回。
// 需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
)

func TestPgvectorRetriever_IndexAndSearch(t *testing.T) {
	pool := testpg.Start(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE kbs, kb_documents, kb_chunks CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	var kbID, docID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO kbs (id, workspace_id, name) VALUES (gen_random_uuid(), 'ws', 'faq') RETURNING id::text`).Scan(&kbID); err != nil {
		t.Fatalf("insert kb: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO kb_documents (id, kb_id, source_type, status) VALUES (gen_random_uuid(), $1, 'upload', 'pending') RETURNING id::text`, kbID).Scan(&docID); err != nil {
		t.Fatalf("insert doc: %v", err)
	}

	emb := NewLocalEmbedder(1536) // 与 kb_chunks.embedding vector(1536) 同维
	r := NewPgvectorRetriever(pool, emb)

	texts := []string{
		"退款政策:七天内可申请退款,款项原路退回。",
		"登录方式:使用企业邮箱与密码登录控制台。",
	}
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	chunks := make([]Chunk, len(texts))
	for i, txt := range texts {
		chunks[i] = Chunk{ID: "x", DocID: docID, Ordinal: i, Content: txt, Embedding: vecs[i]}
	}
	if err := r.Index(ctx, kbID, chunks); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// 检索「怎么退款」应把退款那条排在前面。
	passages, err := r.Search(ctx, kbID, "怎么退款", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(passages) == 0 {
		t.Fatal("expected passages, got none")
	}
	if !strings.Contains(passages[0].Text, "退款") {
		t.Fatalf("expected refund chunk on top, got: %q", passages[0].Text)
	}

	// 幂等:再 Index 同一 doc 不应翻倍。
	if err := r.Index(ctx, kbID, chunks); err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_chunks WHERE kb_id = $1::uuid`, kbID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 chunks after re-index (idempotent), got %d", n)
	}
}
