package retriever

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

// Embedder 把文本编码为向量。生产用 eino-ext 的 OpenAI embedding 组件
// （设计文档 §7.1），与 LLM Gateway 同一出口。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

// LocalEmbedder 是离线 / 开发 / 测试用的确定性嵌入器：把词袋哈希到固定维度并做
// L2 归一化。它没有语义理解能力，但"共享词越多余弦越高"足以驱动检索链路跑通、
// 让单元测试无需联网。换成真实 embedding 模型只需替换本接口的实现。
type LocalEmbedder struct {
	dim int
}

// NewLocalEmbedder 创建本地嵌入器。dim 一般取 256/512/1536。
func NewLocalEmbedder(dim int) *LocalEmbedder {
	if dim <= 0 {
		dim = 256
	}
	return &LocalEmbedder{dim: dim}
}

func (e *LocalEmbedder) Dim() int { return e.dim }

func (e *LocalEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.embedOne(t)
	}
	return out, nil
}

func (e *LocalEmbedder) embedOne(text string) []float32 {
	vec := make([]float32, e.dim)
	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		idx := int(h.Sum32()) % e.dim
		if idx < 0 {
			idx += e.dim
		}
		vec[idx] += 1
	}
	// L2 归一化，便于用点积当余弦。
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}
	return vec
}

// cosine 计算两个等长向量的余弦相似度（假定已归一化时等价于点积）。
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// tokenize 是一个朴素分词器：小写化 + 按非字母数字切分。中文按字切分。
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cur.WriteRune(r)
		case r > 127: // 非 ASCII（含中文）：逐字成 token
			flush()
			tokens = append(tokens, string(r))
		default:
			flush()
		}
	}
	flush()
	return tokens
}
