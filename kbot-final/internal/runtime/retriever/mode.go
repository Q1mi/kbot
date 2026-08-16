package retriever

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// 检索模式用于 Admin Console 的 BM25、向量与混合检索对比。
const (
	ModeBM25   = "bm25"   // 纯 BM25 关键词
	ModeVector = "vector" // 纯向量(余弦)
	ModeHybrid = "hybrid" // BM25 + 向量 + RRF(+ rerank),即默认 Search
)

// ModeSearcher 是 Searcher 的可选扩展:按指定模式检索。
// 不在核心 Searcher 接口里(避免影响既有调用方);kb.Service 用类型断言探测,未实现则回退 Hybrid。
type ModeSearcher interface {
	SearchMode(ctx context.Context, kbID, query string, k int, mode string) ([]Passage, error)
}

var _ ModeSearcher = (*Retriever)(nil)
var _ ModeSearcher = (*PgvectorRetriever)(nil)

// SearchMode 内存版:bm25/vector 走对应单路,其余按 hybrid。
func (r *Retriever) SearchMode(ctx context.Context, kbID, query string, k int, mode string) ([]Passage, error) {
	if k <= 0 {
		k = 5
	}
	// 与 Search 一致:读 idx 全程持读锁,避免与 Index 写锁竞争(详见 hybrid.go 的 Search)。
	switch mode {
	case ModeBM25:
		r.mu.RLock()
		defer r.mu.RUnlock()
		idx := r.indexes[kbID]
		if idx == nil || len(idx.chunks) == 0 {
			return nil, nil
		}
		return bm25Search(idx, query, k), nil
	case ModeVector:
		// 查询向量化(网络 I/O)放锁外。
		embs, err := r.embedder.Embed(ctx, []string{query})
		if err != nil {
			return nil, err
		}
		r.mu.RLock()
		defer r.mu.RUnlock()
		idx := r.indexes[kbID]
		if idx == nil || len(idx.chunks) == 0 {
			return nil, nil
		}
		return vectorRank(embs[0], idx, k), nil
	default:
		return r.Search(ctx, kbID, query, k)
	}
}

// SearchMode pgvector 版:bm25/vector 各跑一路单独 SQL,其余按 hybrid(RRF)。
func (r *PgvectorRetriever) SearchMode(ctx context.Context, kbID, query string, k int, mode string) ([]Passage, error) {
	if k <= 0 {
		k = 5
	}
	switch mode {
	case ModeBM25:
		return r.bm25Only(ctx, kbID, query, k)
	case ModeVector:
		return r.vectorOnly(ctx, kbID, query, k)
	default:
		return r.Search(ctx, kbID, query, k)
	}
}

func (r *PgvectorRetriever) bm25Only(ctx context.Context, kbID, query string, k int) ([]Passage, error) {
	kbUUID, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	rows, err := r.db.Query(ctx, `
		SELECT id::text, doc_id::text, content, ts_rank_cd(tsv, plainto_tsquery('simple', $1)) AS score
		FROM kb_chunks
		WHERE kb_id = $2 AND tsv @@ plainto_tsquery('simple', $1)
		ORDER BY score DESC
		LIMIT $3`, query, kbUUID, k)
	if err != nil {
		return nil, fmt.Errorf("bm25 query: %w", err)
	}
	defer rows.Close()
	return scanPassages(rows)
}

func (r *PgvectorRetriever) vectorOnly(ctx context.Context, kbID, query string, k int) ([]Passage, error) {
	kbUUID, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	embs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	qvec := vectorLiteral(embs[0])
	// 余弦距离越小越相似,转成 1-distance 当分数。
	rows, err := r.db.Query(ctx, `
		SELECT id::text, doc_id::text, content, (1 - (embedding <=> $1::vector)) AS score
		FROM kb_chunks
		WHERE kb_id = $2 AND embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $3`, qvec, kbUUID, k)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}
	defer rows.Close()
	return scanPassages(rows)
}

// scanPassages 把 (chunk_id, doc_id, content, score) 行集扫成 Passage 列表。
func scanPassages(rows pgx.Rows) ([]Passage, error) {
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
