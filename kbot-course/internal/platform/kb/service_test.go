package kb

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	markdown "github.com/Q1mi/kbot/internal/connector/markdown_folder"
)

func TestSyncRunsExplicitIngestStagesAndKeepsDocumentID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fulfillment.md")
	if err := os.WriteFile(path, []byte("# Fulfillment\nUse the US warehouse when Shenzhen is empty."), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	base, err := service.Create(t.Context(), "ws-1", "Fulfillment SOP")
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.Sync(t.Context(), "ws-1", base.ID, markdown.New(root))
	if err != nil {
		t.Fatal(err)
	}
	wantStages := []string{"parse", "chunk", "embed", "index", "done"}
	if !slices.Equal(job.Stages, wantStages) || job.Status != "succeeded" {
		t.Fatalf("job = %#v", job)
	}
	first, _ := service.Documents(t.Context(), "ws-1", base.ID)
	if _, err := service.Sync(t.Context(), "ws-1", base.ID, markdown.New(root)); err != nil {
		t.Fatal(err)
	}
	second, _ := service.Documents(t.Context(), "ws-1", base.ID)
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("incremental sync changed unchanged document: %#v %#v", first, second)
	}
}
