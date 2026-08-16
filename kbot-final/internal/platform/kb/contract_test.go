package kb_test

// KB Store 契约测试：memory 与 postgres 跑同一组控制面用例；chunk 检索由 retriever 测试覆盖。

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/util"
)

func runKBStoreContract(t *testing.T, newStore func(t *testing.T) kb.Store) {
	ws := "ws-default"

	newKB := func(ctx context.Context, s kb.Store) *domain.KnowledgeBase {
		k := &domain.KnowledgeBase{ID: util.GenerateID(), WorkspaceID: ws, Name: "docs", ChunkingConfig: `{"size":800}`, EmbeddingModel: "text-embedding-3-small", Status: "active", CreatedBy: "u1"}
		if err := s.CreateKB(ctx, k); err != nil {
			t.Fatalf("CreateKB: %v", err)
		}
		return k
	}

	t.Run("KBCreateGetListStatus", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		k := newKB(ctx, s)
		got, err := s.GetKB(ctx, k.ID)
		if err != nil {
			t.Fatalf("GetKB: %v", err)
		}
		if got.Name != "docs" || got.ChunkingConfig != `{"size":800}` || got.EmbeddingModel != "text-embedding-3-small" {
			t.Fatalf("kb mismatch: %+v", got)
		}
		list, err := s.ListKBs(ctx, ws)
		if err != nil || len(list) != 1 {
			t.Fatalf("ListKBs: %v len=%d", err, len(list))
		}
		if err := s.UpdateKBStatus(ctx, k.ID, "indexing"); err != nil {
			t.Fatalf("UpdateKBStatus: %v", err)
		}
		got, _ = s.GetKB(ctx, k.ID)
		if got.Status != "indexing" {
			t.Fatalf("status not updated: %+v", got)
		}
	})

	t.Run("DocumentUpsertGetList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		k := newKB(ctx, s)
		doc := &domain.KbDocument{ID: util.GenerateID(), KbID: k.ID, SourceType: "upload", SourceURI: "/a.md", Hash: "h1", Classification: "internal", Status: "pending"}
		if err := s.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}
		// upsert 同 ID 改 status + ingested_at。
		ing := time.Now()
		doc.Status = "processed"
		doc.IngestedAt = &ing
		if err := s.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument(update): %v", err)
		}
		got, err := s.GetDocument(ctx, doc.ID)
		if err != nil {
			t.Fatalf("GetDocument: %v", err)
		}
		if got.Status != "processed" || got.IngestedAt == nil || got.IngestedAt.Unix() != ing.Unix() {
			t.Fatalf("doc upsert mismatch: %+v", got)
		}
		docs, err := s.ListDocuments(ctx, k.ID)
		if err != nil || len(docs) != 1 {
			t.Fatalf("ListDocuments: %v len=%d", err, len(docs))
		}
	})

	t.Run("IngestJobCreateUpdateList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		k := newKB(ctx, s)
		doc := &domain.KbDocument{ID: util.GenerateID(), KbID: k.ID, SourceType: "upload", Status: "pending"}
		_ = s.UpsertDocument(ctx, doc)
		job := &domain.KbIngestJob{ID: util.GenerateID(), KbID: k.ID, DocID: doc.ID, Stage: "parse", Retries: 0, StartedAt: time.Now()}
		if err := s.CreateIngestJob(ctx, job); err != nil {
			t.Fatalf("CreateIngestJob: %v", err)
		}
		fin := time.Now()
		boom := "embed failed"
		job.Stage = "done"
		job.Retries = 2
		job.Error = &boom
		job.FinishedAt = &fin
		if err := s.UpdateIngestJob(ctx, job); err != nil {
			t.Fatalf("UpdateIngestJob: %v", err)
		}
		jobs, err := s.ListIngestJobs(ctx, k.ID)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("ListIngestJobs: %v len=%d", err, len(jobs))
		}
		if jobs[0].Stage != "done" || jobs[0].Retries != 2 || jobs[0].Error == nil || *jobs[0].Error != "embed failed" {
			t.Fatalf("ingest job update mismatch: %+v", jobs[0])
		}
	})

	t.Run("ConnectorUpsertList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		k := newKB(ctx, s)
		c := &domain.ConnectorInstance{ID: util.GenerateID(), KbID: k.ID, ConnectorKind: "markdown_folder", ConfigJSON: `{"path":"/tmp/kb"}`, Cursor: ""}
		if err := s.UpsertConnector(ctx, c); err != nil {
			t.Fatalf("UpsertConnector: %v", err)
		}
		c.Cursor = "page-2"
		if err := s.UpsertConnector(ctx, c); err != nil {
			t.Fatalf("UpsertConnector(update): %v", err)
		}
		list, err := s.ListConnectors(ctx, k.ID)
		if err != nil || len(list) != 1 {
			t.Fatalf("ListConnectors: %v len=%d", err, len(list))
		}
		if list[0].Cursor != "page-2" || list[0].ConfigJSON != `{"path":"/tmp/kb"}` {
			t.Fatalf("connector mismatch: %+v", list[0])
		}
	})
}

func TestMemoryKBStore_Contract(t *testing.T) {
	runKBStoreContract(t, func(t *testing.T) kb.Store {
		return platform.NewMemoryKBStore()
	})
}
