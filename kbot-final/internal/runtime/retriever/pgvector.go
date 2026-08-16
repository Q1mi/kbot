package retriever

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Searcher 是 KB 检索/索引的抽象:内存版(*Retriever)与 pgvector 版(*PgvectorRetriever)都实现它。
// platform.NewService 按 db 是否存在选用:db != nil → pgvector(跨进程共享 + 重启持久);否则内存。
type Searcher interface {
	Index(ctx context.Context, kbID string, chunks []Chunk) error
	Search(ctx context.Context, kbID, query string, k int) ([]Passage, error)
	Embedder() Embedder
}

var _ Searcher = (*Retriever)(nil)         // 内存版
var _ Searcher = (*PgvectorRetriever)(nil) // PG 版

// PgvectorRetriever 把 chunk 落 kb_chunks(embedding vector + tsv 生成列),检索在 SQL 里做
// BM25(ts_rank_cd) + 向量(<=> 余弦) + RRF(k=60) 合并。跨进程闭环:worker ingest 写库,server 检索读库。
type PgvectorRetriever struct {
	db       *pgxpool.Pool
	embedder Embedder
	rrfK     int // 默认 60
}

// NewPgvectorRetriever 创建 SQL-backed 检索器。
func NewPgvectorRetriever(db *pgxpool.Pool, embedder Embedder) *PgvectorRetriever {
	return &PgvectorRetriever{db: db, embedder: embedder, rrfK: 60}
}

// Embedder 暴露嵌入器,供 ingest 的 embed 阶段复用同一实现。
func (r *PgvectorRetriever) Embedder() Embedder { return r.embedder }

// Index 把一份文档的全部 chunk 写入 kb_chunks。先删该 doc 旧 chunk 再插(幂等),整体在一个事务里。
func (r *PgvectorRetriever) Index(ctx context.Context, kbID string, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	kbUUID, err := uuid.Parse(kbID)
	if err != nil {
		return fmt.Errorf("parse kb id: %w", err)
	}
	docUUID, err := uuid.Parse(chunks[0].DocID)
	if err != nil {
		return fmt.Errorf("parse doc id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit 成功后是 no-op

	if _, err := tx.Exec(ctx, `DELETE FROM kb_chunks WHERE doc_id = $1`, docUUID); err != nil {
		return fmt.Errorf("delete old chunks: %w", err)
	}
	for _, c := range chunks {
		if _, err := tx.Exec(ctx,
			`INSERT INTO kb_chunks (id, kb_id, doc_id, ordinal, content, embedding, classification)
			 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5::vector, 'internal')`,
			kbUUID, docUUID, c.Ordinal, c.Content, vectorLiteral(c.Embedding)); err != nil {
			return fmt.Errorf("insert chunk %d: %w", c.Ordinal, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Search 在 SQL 里跑 BM25 + 向量 + RRF 合并,返回前 topK。
func (r *PgvectorRetriever) Search(ctx context.Context, kbID, query string, topK int) ([]Passage, error) {
	if topK <= 0 {
		topK = 5
	}
	kbUUID, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	embs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	qvec := vectorLiteral(embs[0])

	rows, err := r.db.Query(ctx, `
		WITH bm25 AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY ts_rank_cd(tsv, plainto_tsquery('simple', $1)) DESC) AS rk
			FROM kb_chunks
			WHERE kb_id = $2 AND tsv @@ plainto_tsquery('simple', $1)
			LIMIT 50
		),
		vec AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY embedding <=> $3::vector) AS rk
			FROM kb_chunks
			WHERE kb_id = $2 AND embedding IS NOT NULL
			ORDER BY embedding <=> $3::vector
			LIMIT 50
		),
		merged AS (
			SELECT id, SUM(1.0 / ($4::float + rk)) AS rrf_score
			FROM (SELECT id, rk FROM bm25 UNION ALL SELECT id, rk FROM vec) u
			GROUP BY id
			ORDER BY rrf_score DESC
			LIMIT $5
		)
		SELECT c.id::text, c.doc_id::text, c.content, m.rrf_score
		FROM merged m JOIN kb_chunks c ON c.id = m.id
		ORDER BY m.rrf_score DESC`,
		query, kbUUID, qvec, r.rrfK, topK)
	if err != nil {
		return nil, fmt.Errorf("rrf query: %w", err)
	}
	defer rows.Close()

	var out []Passage
	for rows.Next() {
		var p Passage
		if err := rows.Scan(&p.ChunkID, &p.DocID, &p.Text, &p.Score); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// vectorLiteral 把 []float32 编码成 pgvector 文本字面量 "[v1,v2,...]",配合 $n::vector 入库/检索,
// 无需注册 pgx 向量 codec。
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
