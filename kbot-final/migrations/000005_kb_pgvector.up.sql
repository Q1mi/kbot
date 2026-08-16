-- 000005_kb_pgvector:知识库 + pgvector(设计文档 §4.5 / §5 Knowledge Bases)
-- vector(1536) 维度固定；修改维度需要新建表并重新写入全部 KB。

CREATE EXTENSION IF NOT EXISTS vector;

-- 字段对齐既有 domain.KnowledgeBase(chunking_config 为不透明 JSON 串,用 TEXT)。
CREATE TABLE kbs (
    id              UUID PRIMARY KEY,
    workspace_id    TEXT NOT NULL,
    name            TEXT NOT NULL,
    chunking_config TEXT NOT NULL DEFAULT '{}',
    embedding_model TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active',          -- active / indexing / error
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX kbs_workspace ON kbs (workspace_id, name);

CREATE TABLE kb_documents (
    id             UUID PRIMARY KEY,
    kb_id          UUID NOT NULL REFERENCES kbs(id) ON DELETE CASCADE,
    source_type    TEXT NOT NULL DEFAULT '',
    source_uri     TEXT NOT NULL DEFAULT '',
    hash           TEXT NOT NULL DEFAULT '',
    classification TEXT NOT NULL DEFAULT 'internal',
    status         TEXT NOT NULL DEFAULT 'pending',
    ingested_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX kb_documents_kb ON kb_documents (kb_id);

-- embedding/tsv 由 ingest 管道写入，用于向量与全文检索。
CREATE TABLE kb_chunks (
    id             UUID PRIMARY KEY,
    kb_id          UUID NOT NULL REFERENCES kbs(id) ON DELETE CASCADE,
    doc_id         UUID NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    ordinal        INT NOT NULL DEFAULT 0,
    content        TEXT NOT NULL,
    embedding      vector(1536),
    tsv            tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,  -- 见脱节处:用生成列代替触发器
    classification TEXT NOT NULL DEFAULT 'internal',
    metadata       JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX kb_chunks_embedding_hnsw ON kb_chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX kb_chunks_tsv_gin ON kb_chunks USING gin (tsv);
CREATE INDEX kb_chunks_kb_doc ON kb_chunks (kb_id, doc_id);

CREATE TABLE kb_ingest_jobs (
    id          UUID PRIMARY KEY,
    kb_id       UUID NOT NULL REFERENCES kbs(id) ON DELETE CASCADE,
    doc_id      UUID NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL DEFAULT 'queued',              -- parse / chunk / embed / index / done
    retries     INT NOT NULL DEFAULT 0,
    error       TEXT,                                        -- nullable
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX kb_ingest_jobs_kb ON kb_ingest_jobs (kb_id);

-- config_json 为不透明 JSON 串,用 TEXT(domain.ConnectorInstance.ConfigJSON 是 string)。
CREATE TABLE connector_instances (
    id             UUID PRIMARY KEY,
    kb_id          UUID NOT NULL REFERENCES kbs(id) ON DELETE CASCADE,
    connector_kind TEXT NOT NULL,
    config_json    TEXT NOT NULL DEFAULT '{}',
    cursor         TEXT NOT NULL DEFAULT '',
    last_sync_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX connector_instances_kb ON connector_instances (kb_id);
