package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpaFileServer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html>SPA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := spaFileServer(root)

	cases := []struct {
		path     string
		wantCode int
		wantBody string // 子串,空表示不校验
	}{
		{"/", http.StatusOK, "SPA"},
		{"/assets/app.js", http.StatusOK, "console.log"},
		{"/agents", http.StatusOK, "SPA"},               // 前端深链 → index.html
		{"/kbs/abc-123", http.StatusOK, "SPA"},          // 嵌套深链 → index.html
		{"/assets/missing.js", http.StatusNotFound, ""}, // 带扩展名未命中 → 404
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.wantCode {
			t.Fatalf("%s: code=%d want %d (body=%s)", c.path, rec.Code, c.wantCode, rec.Body.String())
		}
		if c.wantBody != "" && !strings.Contains(rec.Body.String(), c.wantBody) {
			t.Fatalf("%s: body %q missing %q", c.path, rec.Body.String(), c.wantBody)
		}
	}
}
