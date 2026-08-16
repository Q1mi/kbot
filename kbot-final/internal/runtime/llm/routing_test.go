package llm

import (
	"context"
	"errors"
	"testing"
)

func TestRouteFor_ClassificationRouting(t *testing.T) {
	// 两路 Provider 都在:secret 走本地,其余走云。
	g := &Gateway{
		cloud: provider{system: "openai-compatible", modelID: "deepseek-chat"},
		local: &provider{system: "ollama", modelID: "qwen2.5:7b"},
	}
	cases := map[string]string{
		"secret":       "ollama",
		"confidential": "openai-compatible",
		"internal":     "openai-compatible",
		"public":       "openai-compatible",
		"":             "openai-compatible",
	}
	for cls, want := range cases {
		p, err := g.routeFor(cls)
		if err != nil {
			t.Fatalf("routeFor(%q) returned error: %v", cls, err)
		}
		if got := p.system; got != want {
			t.Fatalf("routeFor(%q).system = %q, want %q", cls, got, want)
		}
	}

	// 无本地 Provider 时 secret 请求必须失败关闭,防止敏感数据泄露到云端。
	gNoLocal := &Gateway{cloud: provider{system: "openai-compatible"}}
	if _, err := gNoLocal.routeFor("secret"); !errors.Is(err, ErrLocalModelRequired) {
		t.Fatalf("无本地时 secret 应返回 ErrLocalModelRequired, got %v", err)
	}
}

func TestClassificationContext_RoundTrip(t *testing.T) {
	ctx := WithClassification(context.Background(), "secret")
	if got := classificationFromContext(ctx); got != "secret" {
		t.Fatalf("classificationFromContext = %q, want secret", got)
	}
	// 空分级不写 key。
	if got := classificationFromContext(WithClassification(context.Background(), "")); got != "" {
		t.Fatalf("empty classification should stay empty, got %q", got)
	}
}
