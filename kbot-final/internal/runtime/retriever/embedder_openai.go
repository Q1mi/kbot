package retriever

import (
	"context"
	"fmt"
	"net/http"
	"time"

	aclopenai "github.com/cloudwego/eino-ext/libs/acl/openai"
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
		client, err := aclopenai.NewEmbeddingClient(context.Background(), &aclopenai.EmbeddingConfig{
			APIKey: apiKey, BaseURL: baseURL, Model: model,
			HTTPClient: &http.Client{Timeout: 30 * time.Second},
		})
		if err != nil {
			return nil, fmt.Errorf("create Eino OpenAI embedder: %w", err)
		}
		return &OpenAIEmbedder{client: client, dim: dim}, nil
	default:
		return nil, fmt.Errorf("未知 KBOT_EMBEDDER=%q(应为 local|openai)", kind)
	}
}

// OpenAIEmbedder 通过 Eino OpenAI ACL 的 EmbeddingClient 调用兼容端点。
type OpenAIEmbedder struct {
	client *aclopenai.EmbeddingClient
	dim    int
}

func (e *OpenAIEmbedder) Dim() int { return e.dim }

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings, err := e.client.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("embeddings: 期望 %d 条,返回 %d 条", len(texts), len(embeddings))
	}
	vecs := make([][]float32, len(embeddings))
	for i, embedding := range embeddings {
		if len(embedding) != e.dim {
			return nil, fmt.Errorf("embeddings: 维度 %d 与配置 %d 不符", len(embedding), e.dim)
		}
		vecs[i] = make([]float32, len(embedding))
		for j, value := range embedding {
			vecs[i][j] = float32(value)
		}
	}
	return vecs, nil
}
