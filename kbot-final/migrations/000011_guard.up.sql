-- 000011_guard:Guard 注入/限流/配额/PII/审批(设计文档 §4.13 / §5 Guard)

CREATE TABLE guard_rules (
    id               UUID PRIMARY KEY,
    kind             TEXT NOT NULL,                        -- injection / pii / quota ...
    hook             TEXT NOT NULL DEFAULT '',             -- OnInput / OnLLMCall / OnToolCall
    pattern_or_model TEXT NOT NULL DEFAULT '',
    action           TEXT NOT NULL DEFAULT 'block',        -- block / warn / redact
    enabled          BOOLEAN NOT NULL DEFAULT true,
    workspace_id     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX guard_rules_ws ON guard_rules (workspace_id, kind);

CREATE TABLE injection_logs (
    id              UUID PRIMARY KEY,
    conversation_id UUID,
    sample          TEXT NOT NULL DEFAULT '',
    rule_id         UUID,
    severity        TEXT NOT NULL DEFAULT 'low',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX injection_logs_conv ON injection_logs (conversation_id, created_at DESC);

CREATE TABLE quota_ledger (
    dimension TEXT NOT NULL,                               -- user / workspace / agent ...
    period    TEXT NOT NULL,                               -- 2026-06 / 2026-06-01 ...
    used      BIGINT NOT NULL DEFAULT 0,
    lim       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (dimension, period)
);

CREATE TABLE pii_policies (
    id         UUID PRIMARY KEY,
    scope      TEXT NOT NULL DEFAULT '',
    rules_json JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE approvals (
    id              UUID PRIMARY KEY,
    conversation_id UUID,
    action          TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',       -- pending / approved / rejected
    approver_id     UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);
CREATE INDEX approvals_status ON approvals (status, created_at DESC);
