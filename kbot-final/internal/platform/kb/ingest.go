package kb

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/Q1mi/kbot/internal/util"
)

// ingestTracer 给 ingest 各 stage 打 kb.ingest.{stage} span(设计文档 §4.11 可观测)。
var ingestTracer = otel.Tracer("kbot/kb/ingest")

// Ingest 阶段（设计文档 §4.5 的状态机）。
const (
	StageParse = "parse"
	StageChunk = "chunk"
	StageEmbed = "embed"
	StageIndex = "index"
	StageDone  = "done"
)

// IngestDocument 跑完一份文档的 ingest 管道：parse → chunk → embed → index → done。
//
// 各 stage 在 worker 内顺序执行，并把状态写入 KbIngestJob。
// 幂等性由 (kb_id, doc_id, hash) 保证：retriever.Index 会先清掉该文档旧 chunk 再写入。
func (s *Service) IngestDocument(ctx context.Context, kbID, documentID, content string) error {
	job := &domain.KbIngestJob{
		ID:        util.GenerateID(),
		KbID:      kbID,
		DocID:     documentID,
		Stage:     StageParse,
		StartedAt: time.Now(),
	}
	if err := s.store.CreateIngestJob(ctx, job); err != nil {
		return fmt.Errorf("create ingest job: %w", err)
	}

	ctx, span := ingestTracer.Start(ctx, "kb.ingest",
		trace.WithAttributes(attribute.String("kb.id", kbID), attribute.String("doc.id", documentID)))
	defer span.End()

	fail := func(stage string, err error) error {
		msg := err.Error()
		job.Stage = stage
		job.Error = &msg
		_ = s.store.UpdateIngestJob(ctx, job)
		_ = s.markDoc(ctx, documentID, "error")
		span.RecordError(err)
		return fmt.Errorf("ingest stage %s: %w", stage, err)
	}

	// setStage:更新 job 状态机 + 开一个 stage 子 span(调用方负责 End)。
	setStage := func(stage string) trace.Span {
		job.Stage = stage
		_ = s.store.UpdateIngestJob(ctx, job)
		_, sp := ingestTracer.Start(ctx, "kb.ingest."+stage)
		return sp
	}

	// 1) parse：当前接收 Markdown 和纯文本。
	sp := setStage(StageParse)
	parsed := content
	sp.End()

	// 2) chunk
	sp = setStage(StageChunk)
	texts := chunkText(parsed, s.chunkSize, s.overlap)
	sp.SetAttributes(attribute.Int("chunks", len(texts)))
	sp.End()
	if len(texts) == 0 {
		return fail(StageChunk, fmt.Errorf("文档切片为空"))
	}

	// 3) embed
	sp = setStage(StageEmbed)
	embs, err := s.retriever.Embedder().Embed(ctx, texts)
	sp.End()
	if err != nil {
		return fail(StageEmbed, err)
	}

	// 4) index
	sp = setStage(StageIndex)
	chunks := make([]retriever.Chunk, len(texts))
	for i, t := range texts {
		chunks[i] = retriever.Chunk{
			ID:        fmt.Sprintf("%s#%d", documentID, i),
			DocID:     documentID,
			Ordinal:   i,
			Content:   t,
			Embedding: embs[i],
		}
	}
	err = s.retriever.Index(ctx, kbID, chunks)
	sp.End()
	if err != nil {
		return fail(StageIndex, err)
	}

	// 5) done
	job.Stage = StageDone
	now := time.Now()
	job.FinishedAt = &now
	_ = s.store.UpdateIngestJob(ctx, job)
	return s.markDoc(ctx, documentID, "processed")
}

func (s *Service) markDoc(ctx context.Context, documentID, status string) error {
	doc, err := s.store.GetDocument(ctx, documentID)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("document %s not found", documentID)
	}
	doc.Status = status
	if status == "processed" {
		now := time.Now()
		doc.IngestedAt = &now
	}
	return s.store.UpsertDocument(ctx, doc)
}

// ListIngestJobs 返回某 KB 的 ingest 任务记录。
func (s *Service) ListIngestJobs(ctx context.Context, kbID string) ([]*domain.KbIngestJob, error) {
	return s.store.ListIngestJobs(ctx, kbID)
}
