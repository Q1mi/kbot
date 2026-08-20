CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id text PRIMARY KEY,
    email text NOT NULL,
    password_hash text NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_uidx ON users (lower(email));

CREATE TABLE workspaces (
    id text PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspace_memberships (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'member', 'viewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, workspace_id)
);

CREATE TABLE agents (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    template text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id)
);

CREATE TABLE agent_versions (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id),
    agent_id text NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id, version),
    UNIQUE (id, workspace_id, agent_id),
    FOREIGN KEY (agent_id, workspace_id) REFERENCES agents (id, workspace_id)
);

CREATE TABLE agent_promotions (
    workspace_id text NOT NULL,
    agent_id text NOT NULL,
    environment text NOT NULL CHECK (environment IN ('dev', 'staging', 'prod')),
    agent_version_id text NOT NULL,
    promoted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, agent_id, environment),
    FOREIGN KEY (agent_version_id, workspace_id, agent_id)
        REFERENCES agent_versions (id, workspace_id, agent_id)
);

CREATE TABLE conversations (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id),
    agent_id text NOT NULL,
    agent_version_id text NOT NULL,
    user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (agent_version_id, workspace_id, agent_id)
        REFERENCES agent_versions (id, workspace_id, agent_id)
);

CREATE TABLE messages (
    id text PRIMARY KEY,
    conversation_id text NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX messages_conversation_created_idx ON messages (conversation_id, created_at, id);
