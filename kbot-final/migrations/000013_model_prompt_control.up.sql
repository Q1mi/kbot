-- 000013_model_prompt_control:
-- 模型账号控制面 + Prompt 原子配置版本 + 灰度发布运行快照。

ALTER TABLE providers
    ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN name TEXT NOT NULL DEFAULT '',
    ADD COLUMN api_key_ciphertext BYTEA,
    ADD COLUMN created_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE UNIQUE INDEX providers_workspace_name
    ON providers (workspace_id, name)
    WHERE workspace_id <> '' AND name <> '';

CREATE TABLE model_deployments (
    id                  UUID PRIMARY KEY,
    workspace_id        TEXT NOT NULL,
    provider_account_id UUID NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    model_name          TEXT NOT NULL,
    region              TEXT NOT NULL DEFAULT '',
    timeout_ms          INT NOT NULL DEFAULT 30000,
    max_retries         INT NOT NULL DEFAULT 1,
    status              TEXT NOT NULL DEFAULT 'active',
    created_by          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE model_profiles (
    id           UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE model_profile_versions (
    id                      UUID PRIMARY KEY,
    profile_id              UUID NOT NULL REFERENCES model_profiles(id) ON DELETE CASCADE,
    version                 INT NOT NULL,
    primary_deployment_id   UUID NOT NULL REFERENCES model_deployments(id),
    fallback_deployment_ids JSONB NOT NULL DEFAULT '[]',
    classification_max      TEXT NOT NULL DEFAULT 'internal',
    created_by              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (profile_id, version)
);

CREATE TABLE project_model_bindings (
    workspace_id            TEXT NOT NULL,
    env                     TEXT NOT NULL,
    model_profile_version_id UUID NOT NULL REFERENCES model_profile_versions(id),
    monthly_budget          NUMERIC NOT NULL DEFAULT 0,
    rpm_limit               INT NOT NULL DEFAULT 0,
    tpm_limit               INT NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, env)
);

CREATE TABLE prompt_version_configs (
    prompt_version_id       UUID PRIMARY KEY REFERENCES prompt_versions(id) ON DELETE CASCADE,
    model_profile_version_id UUID REFERENCES model_profile_versions(id),
    generation_config       JSONB NOT NULL DEFAULT '{}'
);

ALTER TABLE prompt_experiments
    ADD COLUMN baseline_version_id UUID REFERENCES prompt_versions(id),
    ADD COLUMN candidate_version_id UUID REFERENCES prompt_versions(id),
    ADD COLUMN traffic_percent INT NOT NULL DEFAULT 0,
    ADD COLUMN created_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN completed_at TIMESTAMPTZ;
ALTER TABLE prompt_experiments
    DROP CONSTRAINT IF EXISTS prompt_experiments_prompt_id_env_key;
CREATE UNIQUE INDEX prompt_experiments_one_active
    ON prompt_experiments (prompt_id, env)
    WHERE status = 'active';

CREATE TABLE prompt_rollout_events (
    id            UUID PRIMARY KEY,
    experiment_id UUID NOT NULL REFERENCES prompt_experiments(id) ON DELETE CASCADE,
    action        TEXT NOT NULL,
    from_traffic  INT NOT NULL DEFAULT 0,
    to_traffic    INT NOT NULL DEFAULT 0,
    actor         TEXT NOT NULL DEFAULT '',
    detail        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX prompt_rollout_events_experiment
    ON prompt_rollout_events (experiment_id, created_at);

CREATE TABLE conversation_runtime_configs (
    conversation_id UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    config_json     JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE model_call_logs
    ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN prompt_version_id UUID,
    ADD COLUMN model_profile_version_id UUID,
    ADD COLUMN deployment_id UUID,
    ADD COLUMN experiment_id UUID,
    ADD COLUMN experiment_variant TEXT NOT NULL DEFAULT '';
