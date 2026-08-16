-- KB:kbs / kb_documents / kb_ingest_jobs / connector_instances
-- kb_chunks 的写入与向量/全文检索由 runtime/retriever 的 PostgreSQL 实现负责。

-- name: GetKB :one
SELECT * FROM kbs WHERE id = $1 LIMIT 1;

-- name: ListKBs :many
SELECT * FROM kbs WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateKB :one
INSERT INTO kbs (id, workspace_id, name, chunking_config, embedding_model, status, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
RETURNING *;

-- name: UpdateKBStatus :exec
UPDATE kbs SET status = $2, updated_at = now() WHERE id = $1;

-- name: UpsertDocument :exec
INSERT INTO kb_documents (id, kb_id, source_type, source_uri, hash, classification, status, ingested_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (id) DO UPDATE
SET source_type = EXCLUDED.source_type, source_uri = EXCLUDED.source_uri, hash = EXCLUDED.hash,
    classification = EXCLUDED.classification, status = EXCLUDED.status, ingested_at = EXCLUDED.ingested_at;

-- name: GetDocument :one
SELECT * FROM kb_documents WHERE id = $1 LIMIT 1;

-- name: ListDocuments :many
SELECT * FROM kb_documents WHERE kb_id = $1 ORDER BY created_at;

-- name: CreateIngestJob :exec
INSERT INTO kb_ingest_jobs (id, kb_id, doc_id, stage, retries, error, started_at, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateIngestJob :exec
UPDATE kb_ingest_jobs SET stage = $2, retries = $3, error = $4, finished_at = $5 WHERE id = $1;

-- name: ListIngestJobs :many
SELECT * FROM kb_ingest_jobs WHERE kb_id = $1 ORDER BY started_at;

-- name: UpsertConnector :exec
INSERT INTO connector_instances (id, kb_id, connector_kind, config_json, cursor, last_sync_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (id) DO UPDATE
SET connector_kind = EXCLUDED.connector_kind, config_json = EXCLUDED.config_json,
    cursor = EXCLUDED.cursor, last_sync_at = EXCLUDED.last_sync_at;

-- name: ListConnectors :many
SELECT * FROM connector_instances WHERE kb_id = $1 ORDER BY created_at;
