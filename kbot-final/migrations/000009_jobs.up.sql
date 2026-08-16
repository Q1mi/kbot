-- 000009_jobs:背景任务系统(设计文档 §4.10 / §5 Jobs)

CREATE TABLE jobs (
    id              UUID PRIMARY KEY,
    workspace_id    TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INT NOT NULL DEFAULT 0,
    scheduled_at    TIMESTAMPTZ,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    error           TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT
);
-- 幂等键唯一(NULL 视为各不相同,可多条无键任务)
CREATE UNIQUE INDEX jobs_idempotency_key ON jobs (idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX jobs_status ON jobs (status, scheduled_at);

CREATE TABLE job_schedules (
    id          UUID PRIMARY KEY,
    type        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    cron        TEXT NOT NULL,
    next_run_at TIMESTAMPTZ,
    enabled     BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE dead_letters (
    id      UUID PRIMARY KEY,
    job_id  UUID,
    payload JSONB NOT NULL DEFAULT '{}',
    error   TEXT NOT NULL DEFAULT '',
    dlq_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
