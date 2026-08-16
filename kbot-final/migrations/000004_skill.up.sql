-- 000004_skill:Skills 编辑器与市场(设计文档 §4.4 / §5 Skills)

CREATE TABLE skills (
    id           UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX skills_workspace_name ON skills (workspace_id, name);

-- 字段对齐既有 domain.SkillVersion(frontmatter_json/body_md 均为字符串,用 TEXT;allowed_tools 由 frontmatter 解析,不单列)。
CREATE TABLE skill_versions (
    id               UUID PRIMARY KEY,
    skill_id         UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    version          INT NOT NULL,
    frontmatter_json TEXT NOT NULL DEFAULT '{}',
    body_md          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'draft',          -- draft / published
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (skill_id, version)
);
CREATE INDEX skill_versions_skill_ver ON skill_versions (skill_id, version DESC);

-- skill_subscriptions 对齐 domain.SkillSubscription(skill_id+version_id+agent_id+workspace_id);memory 版按 agent 追加,故不设唯一约束。
CREATE TABLE skill_subscriptions (
    id           BIGSERIAL PRIMARY KEY,
    skill_id     UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    version_id   UUID NOT NULL REFERENCES skill_versions(id) ON DELETE CASCADE,
    agent_id     TEXT NOT NULL DEFAULT '',
    workspace_id TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX skill_subscriptions_agent ON skill_subscriptions (agent_id);
