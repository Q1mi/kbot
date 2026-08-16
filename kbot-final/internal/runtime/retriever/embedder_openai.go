package retriever

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// NewEmbedder 按配置构造嵌入器。
//
//	kind="local"  → 离线确定性 LocalEmbedder(dim)，无需网络,make up 即开即用;
//	kind="openai" → OpenAI 兼容的 /embeddings 端点(与 LLM Gateway 同出口)。
//
// dim 必须与 kb_chunks.embedding vector(N) 一致;不一致会在 platform.NewService 启动时 log.Fatal。
func NewEmbedder(kind string, dim int, baseURL, apiKey, model string) (Embedder, error) {
	switch kind {
	case "", "local":
		return NewLocalEmbedder(dim), nil
	case "openai":
		if baseURL == "" || apiKey == "" {
			return nil, fmt.Errorf("openai embedder 需要 KBOT_LLM_BASE_URL 与 KBOT_LLM_API_KEY")
		}
		return &OpenAIEmbedder{baseURL: baseURL, apiKey: apiKey, model: model, dim: dim, hc: &http.Client{Timeout: 30 * time.Second}}, nil
	default:
		return nil, fmt.Errorf("未知 KBOT_EMBEDDER=%q(应为 local|openai)", kind)
	}
}

// OpenAIEmbedder 调用 OpenAI 兼容的 /embeddings 端点(text-embedding-3-small 等)。
type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	dim     int
	hc      *http.Client
}

func (e *OpenAIEmbedder) Dim() int { return e.dim }

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings endpoint status %d", resp.StatusCode)
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: 期望 %d 条,返回 %d 条", len(texts), len(out.Data))
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		if len(d.Embedding) != e.dim {
			return nil, fmt.Errorf("embeddings: 维度 %d 与配置 %d 不符", len(d.Embedding), e.dim)
		}
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
