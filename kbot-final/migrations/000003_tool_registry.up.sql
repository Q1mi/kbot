-- 000003_tool_registry:工具注册中心 + Sandbox(设计文档 §4.3 / §5 Tools)

CREATE TABLE tools (
    id                 UUID PRIMARY KEY,
    workspace_id       TEXT NOT NULL,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    source_type        TEXT NOT NULL CHECK (source_type IN ('rest_api','mcp_server','internal_sdk','code_execution','a2a')),
    sensitive          BOOLEAN NOT NULL DEFAULT false,        -- 敏感工具触发人在环审批
    classification_max TEXT NOT NULL DEFAULT 'internal',      -- 可处理的最高数据级别
    created_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tools_workspace_name ON tools (workspace_id, name);

-- 字段对齐既有 domain.ToolVersion(schema_json/endpoint_config/auth_config/retry_policy 均为不透明 JSON 字符串,用 TEXT)。
-- 工具用「当前版本 = 最新版本号」语义(domain 无 env 指针),故不建 tool_envs。
CREATE TABLE tool_versions (
    id              UUID PRIMARY KEY,
    tool_id         UUID NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    version         INT NOT NULL,
    schema_json     TEXT NOT NULL DEFAULT '{}',
    endpoint_config TEXT NOT NULL DEFAULT '{}',
    auth_config     TEXT NOT NULL DEFAULT '{}',
    retry_policy    TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'draft',           -- draft / published / deprecated
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tool_id, version)
);
CREATE INDEX tool_versions_tool_ver ON tool_versions (tool_id, version DESC);

-- 保留早期 tool_permissions schema 以兼容已有数据库；当前运行时使用 Agent/Skill 版本快照中的 allowlist。
CREATE TABLE tool_permissions (
    id           BIGSERIAL PRIMARY KEY,
    tool_id      UUID NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL DEFAULT '',
    skill_id     TEXT NOT NULL DEFAULT '',
    agent_id     TEXT NOT NULL DEFAULT '',
    allowed      BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tool_permissions_tool ON tool_permissions (tool_id);

CREATE TABLE tool_test_runs (
    id         UUID PRIMARY KEY,
    tool_id    UUID NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    input      TEXT NOT NULL DEFAULT '',
    output     TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT '',                     -- success / error
    latency_ms INT NOT NULL DEFAULT 0,
    error      TEXT,                                         -- nullable:成功时为 NULL
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tool_test_runs_tool ON tool_test_runs (tool_id, created_at DESC);

CREATE TABLE tool_invocations (
    id              UUID PRIMARY KEY,
    conversation_id UUID,
    tool_version_id UUID REFERENCES tool_versions(id) ON DELETE SET NULL,
    args            JSONB NOT NULL DEFAULT '{}',
    result          JSONB NOT NULL DEFAULT '{}',
    latency_ms      INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tool_invocations_conv ON tool_invocations (conversation_id, created_at DESC);

CREATE TABLE sandbox_executions (
    id             UUID PRIMARY KEY,
    tool_id        UUID REFERENCES tools(id) ON DELETE SET NULL,
    container_id   TEXT NOT NULL DEFAULT '',
    exit_code      INT,
    stdout         TEXT NOT NULL DEFAULT '',
    stderr         TEXT NOT NULL DEFAULT '',
    resource_usage JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
