// Package retriever 实现 BM25、向量、RRF 与 rerank 混合检索。
// Retriever 供测试和单进程装配使用，PgvectorRetriever 提供持久化实现。
package retriever

import (
	"context"
	"math"
	"sort"
	"sync"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/flow/retriever/router"
	"github.com/cloudwego/eino/schema"
)

// Passage 是一条检索回的片段，带来源 doc_id 与分数，便于回答时标注引用。
type Passage struct {
	DocID   string  `json:"doc_id"`
	ChunkID string  `json:"chunk_id"`
	Text    string  `json:"text"`
	Score   float64 `json:"score"`
}

// Chunk 是写入索引的最小单位。
type Chunk struct {
	ID        string
	DocID     string
	Ordinal   int
	Content   string
	Embedding []float32
}

// indexedChunk 是索引内部表示，预存分词结果与词频。
type indexedChunk struct {
	Chunk
	terms map[string]int // term -> tf
	len   int            // 文档词数
}

// kbIndex 是单个 KB 的进程内索引。
type kbIndex struct {
	chunks   []*indexedChunk
	df       map[string]int // term -> 含该 term 的文档数
	totalLen int
}

// Retriever 持有所有 KB 的索引与嵌入器。
type Retriever struct {
	mu       sync.RWMutex
	embedder Embedder
	indexes  map[string]*kbIndex // kbID -> index
}

// New 创建检索器。
func New(embedder Embedder) *Retriever {
	return &Retriever{
		embedder: embedder,
		indexes:  make(map[string]*kbIndex),
	}
}

// Embedder 暴露嵌入器，供 KB ingest 的 embed 阶段复用同一实现。
func (r *Retriever) Embedder() Embedder { return r.embedder }

// Index 把一批 chunk 写入指定 KB 的索引（KB ingest 的 index 阶段调用）。
// 重复 index 同一 docID 会先清掉该文档的旧 chunk，保证幂等。
func (r *Retriever) Index(ctx context.Context, kbID string, chunks []Chunk) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := r.indexes[kbID]
	if idx == nil {
		idx = &kbIndex{df: make(map[string]int)}
		r.indexes[kbID] = idx
	}

	// 收集本批涉及的 docID，先删旧 chunk（幂等）。
	affected := map[string]bool{}
	for _, c := range chunks {
		affected[c.DocID] = true
	}
	idx.removeDocs(affected)

	for _, c := range chunks {
		terms := termFreq(c.Content)
		ic := &indexedChunk{Chunk: c, terms: terms, len: tokenCount(terms)}
		idx.chunks = append(idx.chunks, ic)
		idx.totalLen += ic.len
		for term := range terms {
			idx.df[term]++
		}
	}
	return nil
}

func (idx *kbIndex) removeDocs(docIDs map[string]bool) {
	if len(idx.chunks) == 0 {
		return
	}
	kept := idx.chunks[:0:0]
	for _, c := range idx.chunks {
		if docIDs[c.DocID] {
			idx.totalLen -= c.len
			for term := range c.terms {
				idx.df[term]--
				if idx.df[term] <= 0 {
					delete(idx.df, term)
				}
			}
			continue
		}
		kept = append(kept, c)
	}
	idx.chunks = kept
}

// Search 执行混合检索：向量与 BM25 两路 → RRF 合并 → rerank 收尾。
//
// 并发安全:查询向量化是唯一的网络 I/O,放在【锁外】做;其余全是读 idx 的纯内存计算,
// 全程持读锁——与 Index 的写锁互斥,杜绝"一边检索读 idx.chunks/df/totalLen、一边 ingest 改它们"
// 的数据竞争(尤其 idx.df 的并发读写会直接 fatal)。把 embed 移出锁,避免持锁期间阻塞 ingest。
func (r *Retriever) Search(ctx context.Context, kbID, query string, k int) ([]Passage, error) {
	if k <= 0 {
		k = 5
	}

	// 1) 锁外:查询向量化(网络 I/O,不碰 idx)。
	embs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	qv := embs[0]

	// 2) 锁内:所有对 idx 的读 + 排序合并都在读锁保护下完成。
	r.mu.RLock()
	defer r.mu.RUnlock()

	idx := r.indexes[kbID]
	if idx == nil || len(idx.chunks) == 0 {
		return nil, nil
	}

	cand := k * 2 // 两路各取 k*2 候选。
	hybrid, err := router.NewRetriever(ctx, &router.Config{
		Retrievers: map[string]einoretriever.Retriever{
			"vector": &rankedRetriever{rank: func(_ string, topK int) []Passage { return vectorRank(qv, idx, topK) }},
			"bm25":   &rankedRetriever{rank: func(q string, topK int) []Passage { return bm25Search(idx, q, topK) }},
		},
		Router: func(context.Context, string) ([]string, error) {
			return []string{"vector", "bm25"}, nil
		},
	})
	if err != nil {
		return nil, err
	}
	documents, err := hybrid.Retrieve(ctx, query, einoretriever.WithTopK(cand))
	if err != nil {
		return nil, err
	}
	merged := make([]Passage, 0, len(documents))
	for _, document := range documents {
		docID, _ := document.MetaData["doc_id"].(string)
		merged = append(merged, Passage{DocID: docID, ChunkID: document.ID, Text: document.Content})
		if len(merged) == cand {
			break
		}
	}

	return r.rerank(query, merged, k), nil
}

type rankedRetriever struct {
	rank func(query string, topK int) []Passage
}

func (r *rankedRetriever) Retrieve(
	_ context.Context, query string, opts ...einoretriever.Option,
) ([]*schema.Document, error) {
	topK := 10
	if configured := einoretriever.GetCommonOptions(nil, opts...).TopK; configured != nil && *configured > 0 {
		topK = *configured
	}
	passages := r.rank(query, topK)
	documents := make([]*schema.Document, 0, len(passages))
	for _, passage := range passages {
		documents = append(documents, &schema.Document{
			ID: passage.ChunkID, Content: passage.Text,
			MetaData: map[string]any{"doc_id": passage.DocID, "score": passage.Score},
		})
	}
	return documents, nil
}

// vectorRank 用余弦相似度对所有 chunk 排序，返回前 n 个。
// 调用方已持有读锁并备好查询向量 qv（故不再做 embed、不需 ctx）。
func vectorRank(qv []float32, idx *kbIndex, n int) []Passage {
	scored := make([]Passage, 0, len(idx.chunks))
	for _, c := range idx.chunks {
		s := cosine(qv, c.Embedding)
		scored = append(scored, Passage{DocID: c.DocID, ChunkID: c.ID, Text: c.Content, Score: s})
	}
	sortByScoreDesc(scored)
	if len(scored) > n {
		scored = scored[:n]
	}
	return scored
}

// rerank 使用词重叠相关度作为轻量收尾信号。
func (r *Retriever) rerank(query string, passages []Passage, k int) []Passage {
	qTerms := termFreq(query)
	for i := range passages {
		passages[i].Score = overlapScore(qTerms, termFreq(passages[i].Text))
	}
	sortByScoreDesc(passages)
	if len(passages) > k {
		passages = passages[:k]
	}
	return passages
}

func overlapScore(q, d map[string]int) float64 {
	if len(q) == 0 {
		return 0
	}
	var hit int
	for term := range q {
		if d[term] > 0 {
			hit++
		}
	}
	return float64(hit) / float64(len(q))
}

func sortByScoreDesc(p []Passage) {
	sort.SliceStable(p, func(i, j int) bool { return p[i].Score > p[j].Score })
}

// --- BM25 ---

func bm25Search(idx *kbIndex, query string, n int) []Passage {
	const k1 = 1.5
	const b = 0.75
	N := float64(len(idx.chunks))
	if N == 0 {
		return nil
	}
	avgdl := float64(idx.totalLen) / N

	qTerms := termFreq(query)
	scored := make([]Passage, 0, len(idx.chunks))
	for _, c := range idx.chunks {
		var score float64
		for term := range qTerms {
			tf := float64(c.terms[term])
			if tf == 0 {
				continue
			}
			df := float64(idx.df[term])
			idf := math.Log(1 + (N-df+0.5)/(df+0.5))
			denom := tf + k1*(1-b+b*float64(c.len)/avgdl)
			score += idf * (tf * (k1 + 1)) / denom
		}
		if score > 0 {
			scored = append(scored, Passage{DocID: c.DocID, ChunkID: c.ID, Text: c.Content, Score: score})
		}
	}
	sortByScoreDesc(scored)
	if len(scored) > n {
		scored = scored[:n]
	}
	return scored
}

func termFreq(s string) map[string]int {
	tf := map[string]int{}
	for _, tok := range tokenize(s) {
		tf[tok]++
	}
	return tf
}

func tokenCount(tf map[string]int) int {
	var n int
	for _, c := range tf {
		n += c
	}
	return n
}
