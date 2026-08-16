package kb

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 KB 控制面存储；kb_chunks 的写入与检索由 retriever 负责。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

// ---- KnowledgeBase ----

func (s *PostgresStore) CreateKB(ctx context.Context, kb *domain.KnowledgeBase) error {
	id, err := uuid.Parse(kb.ID)
	if err != nil {
		return fmt.Errorf("parse kb id: %w", err)
	}
	if _, err := s.q.CreateKB(ctx, pgstore.CreateKBParams{
		ID:             id,
		WorkspaceID:    kb.WorkspaceID,
		Name:           kb.Name,
		ChunkingConfig: orEmpty(kb.ChunkingConfig, "{}"),
		EmbeddingModel: kb.EmbeddingModel,
		Status:         orEmpty(kb.Status, "active"),
		CreatedBy:      kb.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create kb: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetKB(ctx context.Context, kbID string) (*domain.KnowledgeBase, error) {
	id, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	row, err := s.q.GetKB(ctx, id)
	if err != nil {
		return nil, notFound("kb", err)
	}
	return kbFromRow(row), nil
}

func (s *PostgresStore) ListKBs(ctx context.Context, workspaceID string) ([]*domain.KnowledgeBase, error) {
	rows, err := s.q.ListKBs(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list kbs: %w", err)
	}
	out := make([]*domain.KnowledgeBase, 0, len(rows))
	for _, r := range rows {
		out = append(out, kbFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) UpdateKBStatus(ctx context.Context, kbID, status string) error {
	id, err := uuid.Parse(kbID)
	if err != nil {
		return fmt.Errorf("parse kb id: %w", err)
	}
	if err := s.q.UpdateKBStatus(ctx, pgstore.UpdateKBStatusParams{ID: id, Status: status}); err != nil {
		return fmt.Errorf("update kb status: %w", err)
	}
	return nil
}

// ---- Document ----

func (s *PostgresStore) UpsertDocument(ctx context.Context, doc *domain.KbDocument) error {
	id, err := uuid.Parse(doc.ID)
	if err != nil {
		return fmt.Errorf("parse doc id: %w", err)
	}
	kbID, err := uuid.Parse(doc.KbID)
	if err != nil {
		return fmt.Errorf("parse kb id: %w", err)
	}
	if err := s.q.UpsertDocument(ctx, pgstore.UpsertDocumentParams{
		ID:             id,
		KbID:           kbID,
		SourceType:     doc.SourceType,
		SourceUri:      doc.SourceURI,
		Hash:           doc.Hash,
		Classification: orEmpty(doc.Classification, "internal"),
		Status:         orEmpty(doc.Status, "pending"),
		IngestedAt:     tsFromPtr(doc.IngestedAt),
	}); err != nil {
		return fmt.Errorf("upsert document: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDocument(ctx context.Context, docID string) (*domain.KbDocument, error) {
	id, err := uuid.Parse(docID)
	if err != nil {
		return nil, fmt.Errorf("parse doc id: %w", err)
	}
	row, err := s.q.GetDocument(ctx, id)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}
	return docFromRow(row), nil
}

func (s *PostgresStore) ListDocuments(ctx context.Context, kbID string) ([]*domain.KbDocument, error) {
	id, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	rows, err := s.q.ListDocuments(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	out := make([]*domain.KbDocument, 0, len(rows))
	for _, r := range rows {
		out = append(out, docFromRow(r))
	}
	return out, nil
}

// ---- IngestJob ----

func (s *PostgresStore) CreateIngestJob(ctx context.Context, job *domain.KbIngestJob) error {
	id, err := uuid.Parse(job.ID)
	if err != nil {
		return fmt.Errorf("parse job id: %w", err)
	}
	kbID, err := uuid.Parse(job.KbID)
	if err != nil {
		return fmt.Errorf("parse kb id: %w", err)
	}
	docID, err := uuid.Parse(job.DocID)
	if err != nil {
		return fmt.Errorf("parse doc id: %w", err)
	}
	started := job.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	if err := s.q.CreateIngestJob(ctx, pgstore.CreateIngestJobParams{
		ID:         id,
		KbID:       kbID,
		DocID:      docID,
		Stage:      orEmpty(job.Stage, "queued"),
		Retries:    int32(job.Retries),
		Error:      textFromStrPtr(job.Error),
		StartedAt:  started,
		FinishedAt: tsFromPtr(job.FinishedAt),
	}); err != nil {
		return fmt.Errorf("create ingest job: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateIngestJob(ctx context.Context, job *domain.KbIngestJob) error {
	id, err := uuid.Parse(job.ID)
	if err != nil {
		return fmt.Errorf("parse job id: %w", err)
	}
	if err := s.q.UpdateIngestJob(ctx, pgstore.UpdateIngestJobParams{
		ID:         id,
		Stage:      job.Stage,
		Retries:    int32(job.Retries),
		Error:      textFromStrPtr(job.Error),
		FinishedAt: tsFromPtr(job.FinishedAt),
	}); err != nil {
		return fmt.Errorf("update ingest job: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListIngestJobs(ctx context.Context, kbID string) ([]*domain.KbIngestJob, error) {
	id, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	rows, err := s.q.ListIngestJobs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list ingest jobs: %w", err)
	}
	out := make([]*domain.KbIngestJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, ingestJobFromRow(r))
	}
	return out, nil
}

// ---- Connector ----

func (s *PostgresStore) UpsertConnector(ctx context.Context, c *domain.ConnectorInstance) error {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return fmt.Errorf("parse connector id: %w", err)
	}
	kbID, err := uuid.Parse(c.KbID)
	if err != nil {
		return fmt.Errorf("parse kb id: %w", err)
	}
	if err := s.q.UpsertConnector(ctx, pgstore.UpsertConnectorParams{
		ID:            id,
		KbID:          kbID,
		ConnectorKind: c.ConnectorKind,
		ConfigJson:    orEmpty(c.ConfigJSON, "{}"),
		Cursor:        c.Cursor,
		LastSyncAt:    tsFromPtr(c.LastSyncAt),
	}); err != nil {
		return fmt.Errorf("upsert connector: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListConnectors(ctx context.Context, kbID string) ([]*domain.ConnectorInstance, error) {
	id, err := uuid.Parse(kbID)
	if err != nil {
		return nil, fmt.Errorf("parse kb id: %w", err)
	}
	rows, err := s.q.ListConnectors(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	out := make([]*domain.ConnectorInstance, 0, len(rows))
	for _, r := range rows {
		out = append(out, connectorFromRow(r))
	}
	return out, nil
}

// ---- 行 → domain ----

func kbFromRow(r pgstore.Kb) *domain.KnowledgeBase {
	return &domain.KnowledgeBase{
		ID:             r.ID.String(),
		WorkspaceID:    r.WorkspaceID,
		Name:           r.Name,
		ChunkingConfig: r.ChunkingConfig,
		EmbeddingModel: r.EmbeddingModel,
		Status:         r.Status,
		CreatedBy:      r.CreatedBy,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func docFromRow(r pgstore.KbDocument) *domain.KbDocument {
	return &domain.KbDocument{
		ID:             r.ID.String(),
		KbID:           r.KbID.String(),
		SourceType:     r.SourceType,
		SourceURI:      r.SourceUri,
		Hash:           r.Hash,
		Classification: r.Classification,
		Status:         r.Status,
		IngestedAt:     ptrFromTs(r.IngestedAt),
		CreatedAt:      r.CreatedAt,
	}
}

func ingestJobFromRow(r pgstore.KbIngestJob) *domain.KbIngestJob {
	return &domain.KbIngestJob{
		ID:         r.ID.String(),
		KbID:       r.KbID.String(),
		DocID:      r.DocID.String(),
		Stage:      r.Stage,
		Retries:    int(r.Retries),
		Error:      strPtrFromText(r.Error),
		StartedAt:  r.StartedAt,
		FinishedAt: ptrFromTs(r.FinishedAt),
	}
}

func connectorFromRow(r pgstore.ConnectorInstance) *domain.ConnectorInstance {
	return &domain.ConnectorInstance{
		ID:            r.ID.String(),
		KbID:          r.KbID.String(),
		ConnectorKind: r.ConnectorKind,
		ConfigJSON:    r.ConfigJson,
		Cursor:        r.Cursor,
		LastSyncAt:    ptrFromTs(r.LastSyncAt),
		CreatedAt:     r.CreatedAt,
	}
}

// ---- 小工具 ----

func orEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

func textFromStrPtr(p *string) pgtype.Text {
	if p == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *p, Valid: true}
}

func strPtrFromText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func tsFromPtr(p *time.Time) pgtype.Timestamptz {
	if p == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *p, Valid: true}
}

func ptrFromTs(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
