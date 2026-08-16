package cache

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/retriever"
)

func TestEmbeddingCacheHitMiss(t *testing.T) {
	ec := NewEmbeddingCache(retriever.NewLocalEmbedder(64))
	ctx := context.Background()

	// 首次：两条都 miss。
	if _, err := ec.Embed(ctx, []string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}
	// 再来：hello 命中，new 未命中。
	if _, err := ec.Embed(ctx, []string{"hello", "new"}); err != nil {
		t.Fatal(err)
	}
	hits, miss := ec.Stats()
	if hits != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}
	if miss != 3 {
		t.Fatalf("expected 3 misses, got %d", miss)
	}
}

func TestEmbeddingCacheConsistency(t *testing.T) {
	ec := NewEmbeddingCache(retriever.NewLocalEmbedder(64))
	ctx := context.Background()
	a, _ := ec.Embed(ctx, []string{"same"})
	b, _ := ec.Embed(ctx, []string{"same"})
	if len(a[0]) != len(b[0]) {
		t.Fatal("dim mismatch")
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatal("cached vector differs from original")
		}
	}
}

func TestMemoryIdemStore(t *testing.T) {
	s := NewMemoryIdemStore()
	ctx := context.Background()
	if _, ok, _ := s.Get(ctx, "k1"); ok {
		t.Fatal("expected miss")
	}
	_ = s.Set(ctx, "k1", []byte(`{"ok":true}`), time.Hour)
	v, ok, _ := s.Get(ctx, "k1")
	if !ok || string(v) != `{"ok":true}` {
		t.Fatalf("expected hit, got ok=%v v=%s", ok, v)
	}

	v[0] = 'x'
	again, ok, err := s.Get(ctx, "k1")
	if err != nil || !ok || string(again) != `{"ok":true}` {
		t.Fatalf("stored value was mutated: ok=%v err=%v value=%s", ok, err, again)
	}

	if err := s.Set(ctx, "expired", []byte("value"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok, err := s.Get(ctx, "expired"); err != nil || ok {
		t.Fatalf("expired entry returned: ok=%v err=%v", ok, err)
	}
}
