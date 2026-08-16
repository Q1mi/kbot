-- 000008_llm_gateway:LLM 网关 + 数据分级路由(设计文档 §4.6 / §5 LLM Gateway)
-- model_call_logs 按 created_at 月度分区，default 分区承接超出预创建范围的数据。
-- worker 的 maintenance 任务负责创建后续分区。

CREATE TABLE providers (
    id             UUID PRIMARY KEY,
    kind           TEXT NOT NULL,                          -- openai / deepseek / ollama ...
    credential_ref TEXT NOT NULL DEFAULT '',
    base_url       TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE model_aliases (
    id                 UUID PRIMARY KEY,
    alias              TEXT UNIQUE NOT NULL,               -- chat-cloud / chat-local
    provider_id        UUID REFERENCES providers(id) ON DELETE SET NULL,
    model_name         TEXT NOT NULL,
    capabilities       JSONB NOT NULL DEFAULT '{}',
    classification_max TEXT NOT NULL DEFAULT 'internal'    -- 这个模型最高能处理的级别
);

CREATE TABLE routing_policies (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    rules_json JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE model_call_logs (
    id            UUID NOT NULL DEFAULT gen_random_uuid(),
    agent_id      UUID,
    user_id       UUID,
    provider_id   UUID,
    model         TEXT NOT NULL DEFAULT '',
    input_tokens  INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,
    cached_tokens INT NOT NULL DEFAULT 0,
    cost          NUMERIC NOT NULL DEFAULT 0,
    latency_ms    INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT '',
    classification TEXT NOT NULL DEFAULT 'internal',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE TABLE model_call_logs_default PARTITION OF model_call_logs DEFAULT;
CREATE INDEX model_call_logs_agent ON model_call_logs (agent_id, created_at DESC);

CREATE TABLE usage_metrics (
    workspace_id  TEXT NOT NULL,
    agent_id      UUID NOT NULL,
    model         TEXT NOT NULL DEFAULT '',
    minute        TIMESTAMPTZ NOT NULL,
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    calls         BIGINT NOT NULL DEFAULT 0,
    errors        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, agent_id, model, minute)
);
