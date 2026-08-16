-- 000023_model_project_quota：模型计价和项目级 RPM/TPM/月预算强制执行账本。

ALTER TABLE model_deployments
    ADD COLUMN input_price_per_million NUMERIC NOT NULL DEFAULT 0,
    ADD COLUMN output_price_per_million NUMERIC NOT NULL DEFAULT 0,
    ADD COLUMN cached_input_price_per_million NUMERIC NOT NULL DEFAULT 0,
    ADD CONSTRAINT model_deployment_prices_nonnegative CHECK (
        input_price_per_million >= 0 AND output_price_per_million >= 0 AND
        cached_input_price_per_million >= 0
    );

CREATE TABLE project_model_usage_reservations (
    id                       UUID PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    env                      TEXT NOT NULL,
    model_profile_version_id UUID NOT NULL REFERENCES model_profile_versions(id),
    deployment_id            UUID NOT NULL REFERENCES model_deployments(id),
    minute                   TIMESTAMPTZ NOT NULL,
    month                    DATE NOT NULL,
    reserved_tokens          BIGINT NOT NULL DEFAULT 0,
    actual_tokens            BIGINT NOT NULL DEFAULT 0,
    reserved_cost            NUMERIC NOT NULL DEFAULT 0,
    actual_cost              NUMERIC NOT NULL DEFAULT 0,
    status                   TEXT NOT NULL DEFAULT 'reserved',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at             TIMESTAMPTZ,
    CONSTRAINT project_usage_status_valid CHECK (status IN ('reserved', 'completed', 'failed')),
    CONSTRAINT project_usage_values_nonnegative CHECK (
        reserved_tokens >= 0 AND actual_tokens >= 0 AND reserved_cost >= 0 AND actual_cost >= 0
    )
);
CREATE INDEX project_model_usage_minute
    ON project_model_usage_reservations (workspace_id, env, minute);
CREATE INDEX project_model_usage_month
    ON project_model_usage_reservations (workspace_id, env, month);
