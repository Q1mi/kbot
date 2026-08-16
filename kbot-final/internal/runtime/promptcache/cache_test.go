package promptcache

import "testing"

func TestCompileAndRender(t *testing.T) {
	c, err := Compile("v1", "你好 {{.name}}", `{"required":["name"]}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := c.Render(map[string]any{"name": "世界"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "你好 世界" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRenderMissingRequired(t *testing.T) {
	c, _ := Compile("v1", "{{.a}}", `{"required":["a"]}`)
	if _, err := c.Render(map[string]any{}); err == nil {
		t.Fatal("expected error for missing required var")
	}
}

func TestCompileRejectsBadTemplate(t *testing.T) {
	if _, err := Compile("v1", "{{.unclosed", ""); err == nil {
		t.Fatal("expected parse error for bad template")
	}
}

func TestEstimateTokens(t *testing.T) {
	// CJK 约 1 token/字。
	if got := EstimateTokens("你好世界"); got != 4 {
		t.Fatalf("expected 4 for 4 CJK chars, got %d", got)
	}
	// ASCII 约 4 字符/token。
	if got := EstimateTokens("aaaa"); got != 1 {
		t.Fatalf("expected 1 for 4 ascii chars, got %d", got)
	}
}

func TestCacheGetPutInvalidate(t *testing.T) {
	c := NewCache()
	if _, ok := c.Get("p1", "dev"); ok {
		t.Fatal("expected miss")
	}
	comp, _ := Compile("v1", "x", "")
	c.Put("p1", "dev", comp)
	if got, ok := c.Get("p1", "dev"); !ok || got.VersionID != "v1" {
		t.Fatal("expected hit with v1")
	}
	c.Invalidate("p1", "dev")
	if _, ok := c.Get("p1", "dev"); ok {
		t.Fatal("expected miss after invalidate")
	}
}
