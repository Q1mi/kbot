-- 000006_agent_snapshot:Agent Builder + Team(设计文档 §4.7 / §4.9 / §5 Agents)
-- agent_versions.snapshot_json 是 immutable 装配快照(prompt/tool/skill/kb 版本指针)。

CREATE TABLE agents (
    id           UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    template     TEXT NOT NULL DEFAULT 'general',
    description  TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX agents_workspace_name ON agents (workspace_id, name);

-- snapshot_json 为不透明 JSON 串(domain.AgentVersion.SnapshotJSON 是 string),用 TEXT 保真。
CREATE TABLE agent_versions (
    id            UUID PRIMARY KEY,
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version       INT NOT NULL,
    snapshot_json TEXT NOT NULL DEFAULT '{}',               -- immutable:prompt_version_id / tool_version_ids[] / skill_version_ids[] / kb_ids[] / routing_policy_id
    created_by    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, version)
);
CREATE INDEX agent_versions_agent_ver ON agent_versions (agent_id, version DESC);

CREATE TABLE agent_envs (
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    env        TEXT NOT NULL,
    version_id UUID NOT NULL REFERENCES agent_versions(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, env)
);

CREATE TABLE agent_endpoints (
    id          UUID PRIMARY KEY,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    env         TEXT NOT NULL,
    kind        TEXT NOT NULL,                             -- http / ws / webhook
    path        TEXT NOT NULL,
    auth_config JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE teams (
    id           UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    mode         TEXT NOT NULL DEFAULT 'supervisor',       -- supervisor / pipeline
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE team_versions (
    id            UUID PRIMARY KEY,
    team_id       UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    version       INT NOT NULL,
    snapshot_json TEXT NOT NULL DEFAULT '{}',               -- 不透明 JSON(members 等),与 agent_versions 同用 TEXT
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, version)
);

CREATE TABLE team_envs (
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    env        TEXT NOT NULL,
    version_id UUID NOT NULL REFERENCES team_versions(id),
    PRIMARY KEY (team_id, env)
);
