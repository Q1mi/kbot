package markdown_folder

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestListAndFetch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# 文档A\n内容A")
	writeFile(t, filepath.Join(dir, "b.md"), "# 文档B\n内容B")
	writeFile(t, filepath.Join(dir, "ignore.txt"), "plain text")
	// 子目录里的 md 也应被发现。
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "c.md"), "# 文档C")

	c := New(dir)
	if c.Name() != "markdown_folder" {
		t.Fatalf("unexpected name %s", c.Name())
	}

	docs, cursor, err := c.ListDocuments(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if cursor != "" {
		t.Fatalf("expected empty cursor, got %q", cursor)
	}
	if len(docs) != 3 {
		t.Fatalf("expected 3 md docs, got %d", len(docs))
	}
	for _, d := range docs {
		if d.Hash == "" {
			t.Fatalf("doc %s missing hash", d.ID)
		}
	}

	// FetchDocument 能读出内容。
	rc, err := c.FetchDocument(context.Background(), docs[0].ID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer rc.Close()
	content, _ := io.ReadAll(rc)
	if len(content) == 0 {
		t.Fatal("expected content")
	}
}

func TestFetchRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	outside := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-outside.md")
	writeFile(t, outside, "outside")
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, err := c.FetchDocument(context.Background(), outside); err == nil {
		t.Fatal("sibling path with matching prefix was accepted")
	}

	link := filepath.Join(dir, "outside.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchDocument(context.Background(), link); err == nil {
		t.Fatal("symlink escaping the connector root was accepted")
	}
	docs, _, err := c.ListDocuments(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Fatalf("symlink was listed as a document: %+v", docs)
	}
}

func TestHashChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.md")
	writeFile(t, p, "v1")
	c := New(dir)

	docs1, _, _ := c.ListDocuments(context.Background(), "")
	writeFile(t, p, "v2 changed")
	docs2, _, _ := c.ListDocuments(context.Background(), "")

	if docs1[0].Hash == docs2[0].Hash {
		t.Fatal("expected hash to change after content change")
	}
}

func TestCanceledContextStopsConnector(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "content")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := New(dir)
	if _, _, err := c.ListDocuments(ctx, ""); err == nil {
		t.Fatal("ListDocuments accepted a canceled context")
	}
	if _, err := c.FetchDocument(ctx, filepath.Join(dir, "a.md")); err == nil {
		t.Fatal("FetchDocument accepted a canceled context")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
