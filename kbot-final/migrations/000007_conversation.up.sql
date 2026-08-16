-- 000007_conversation:对话与运行时(设计文档 §4.8 / §5 Conversations/Runtime)

-- agent_id/agent_version_id/user_id 用 TEXT(domain.Conversation 对应字段是 string,且 user_id 在 demo 里未必是 UUID)。
CREATE TABLE conversations (
    id               UUID PRIMARY KEY,
    agent_id         TEXT NOT NULL DEFAULT '',
    agent_version_id TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL DEFAULT '',
    workspace_id     TEXT NOT NULL DEFAULT '',
    classification   TEXT NOT NULL DEFAULT 'internal',
    status           TEXT NOT NULL DEFAULT 'active',         -- active / completed / error
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversations_list ON conversations (workspace_id, agent_id, user_id, started_at DESC);

CREATE TABLE messages (
    id              UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,                          -- system / user / assistant / tool
    content         TEXT NOT NULL DEFAULT '',
    tool_calls      JSONB NOT NULL DEFAULT '[]',            -- []domain.ToolCall
    tool_call_id    TEXT,                                   -- nullable:仅 tool 返回消息有
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX messages_conv ON messages (conversation_id, created_at);

CREATE TABLE tool_calls (
    id              UUID PRIMARY KEY,
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_id         UUID,
    tool_version_id UUID,
    args            JSONB NOT NULL DEFAULT '{}',
    result          JSONB NOT NULL DEFAULT '{}',
    latency_ms      INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tool_calls_message ON tool_calls (message_id);

CREATE TABLE skill_triggers (
    id               UUID PRIMARY KEY,
    conversation_id  UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    skill_version_id UUID,
    triggered_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    success          BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE checkpoints (
    id              UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    state_snapshot  JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX checkpoints_conv ON checkpoints (conversation_id, created_at DESC);
