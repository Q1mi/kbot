package markdown_folder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanReturnsStableMarkdownDocuments(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "policy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "policy", "refund.md"), []byte("# 退款政策\n七天内可退款。"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignore.txt"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}
	documents, err := New(root).Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("documents = %+v", documents)
	}
	document := documents[0]
	if document.SourceURI != "policy/refund.md" || document.Title != "退款政策" || document.Checksum == "" {
		t.Fatalf("document = %+v", document)
	}
	again, err := New(root).Scan(context.Background())
	if err != nil || again[0].Checksum != document.Checksum {
		t.Fatalf("checksum is unstable")
	}
}
