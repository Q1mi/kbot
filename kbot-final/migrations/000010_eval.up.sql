-- 000010_eval:评估门禁(设计文档 §4.12 / §5 Eval)

CREATE TABLE judges (
    id        UUID PRIMARY KEY,
    name      TEXT NOT NULL,
    model     TEXT NOT NULL DEFAULT '',
    prompt_id UUID,
    kind      TEXT NOT NULL DEFAULT 'deterministic'      -- deterministic / light / full
        CHECK (kind IN ('deterministic','light','full'))
);

CREATE TABLE eval_datasets (
    id           UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    target_kind  TEXT NOT NULL DEFAULT 'agent',          -- agent / prompt / tool
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX eval_datasets_workspace ON eval_datasets (workspace_id, name);

-- input/expected/metadata 为不透明字符串(domain.EvalCase 对应字段是 string),用 TEXT。
CREATE TABLE eval_cases (
    id         UUID PRIMARY KEY,
    dataset_id UUID NOT NULL REFERENCES eval_datasets(id) ON DELETE CASCADE,
    input      TEXT NOT NULL DEFAULT '',
    expected   TEXT NOT NULL DEFAULT '',
    metadata   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX eval_cases_dataset ON eval_cases (dataset_id);

-- target_id/judge_id 用 TEXT(domain.EvalRun 对应字段是 string,judge 可空);加 threshold;用 created_at(非 started_at)。
CREATE TABLE eval_runs (
    id          UUID PRIMARY KEY,
    dataset_id  UUID NOT NULL REFERENCES eval_datasets(id) ON DELETE CASCADE,
    target_id   TEXT NOT NULL DEFAULT '',
    judge_id    TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'running',            -- running / passed / failed
    pass_rate   DOUBLE PRECISION NOT NULL DEFAULT 0,
    threshold   DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX eval_runs_dataset ON eval_runs (dataset_id, created_at DESC);

-- eval_scores 无独立 ID(domain.EvalScore 无 ID 字段),用 BIGSERIAL 内部主键。
CREATE TABLE eval_scores (
    id        BIGSERIAL PRIMARY KEY,
    run_id    UUID NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
    case_id   UUID NOT NULL REFERENCES eval_cases(id) ON DELETE CASCADE,
    dimension TEXT NOT NULL DEFAULT '',
    score     DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX eval_scores_run ON eval_scores (run_id);
