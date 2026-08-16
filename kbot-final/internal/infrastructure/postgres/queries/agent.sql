-- Agent:agents / agent_versions / agent_envs
-- 「当前版本」走 env 指针(agent_envs);CreateAgentVersion 由 store 自动把新版本绑到 dev(对齐 memory 语义)。

-- name: GetAgent :one
SELECT * FROM agents WHERE id = $1 LIMIT 1;

-- name: ListAgents :many
SELECT * FROM agents WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateAgent :one
INSERT INTO agents (id, workspace_id, name, template, description, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now(), now())
RETURNING *;

-- name: CreateAgentWithVersion :exec
WITH inserted_agent AS (
  INSERT INTO agents (id, workspace_id, name, template, description, created_by, created_at, updated_at)
  VALUES (sqlc.arg(agent_id), sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(template),
          sqlc.arg(description), sqlc.arg(agent_created_by), now(), now())
  RETURNING id
), inserted_version AS (
  INSERT INTO agent_versions (id, agent_id, version, snapshot_json, created_by, created_at)
  SELECT sqlc.arg(version_id), id, 1, sqlc.arg(snapshot_json), sqlc.arg(version_created_by), now()
  FROM inserted_agent
  RETURNING id, agent_id
)
INSERT INTO agent_envs (agent_id, env, version_id, updated_at)
SELECT agent_id, 'dev', id, now() FROM inserted_version;

-- name: GetAgentVersion :one
SELECT * FROM agent_versions WHERE id = $1 LIMIT 1;

-- name: ListAgentVersions :many
SELECT * FROM agent_versions WHERE agent_id = $1 ORDER BY version DESC;

-- name: CreateAgentVersion :one
INSERT INTO agent_versions (id, agent_id, version, snapshot_json, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, now())
RETURNING *;

-- name: CreateAgentVersionAndBindDev :exec
WITH inserted_version AS (
  INSERT INTO agent_versions (id, agent_id, version, snapshot_json, created_by, created_at)
  VALUES (sqlc.arg(version_id), sqlc.arg(agent_id), sqlc.arg(version), sqlc.arg(snapshot_json), sqlc.arg(created_by), now())
  RETURNING id, agent_id
)
INSERT INTO agent_envs (agent_id, env, version_id, updated_at)
SELECT agent_id, 'dev', id, now() FROM inserted_version
ON CONFLICT (agent_id, env) DO UPDATE SET version_id=EXCLUDED.version_id,updated_at=now();

-- name: GetAgentCurrentVersion :one
SELECT av.* FROM agent_versions av
JOIN agent_envs ae ON ae.version_id = av.id
WHERE ae.agent_id = $1 AND ae.env = $2
LIMIT 1;

-- name: UpsertAgentEnv :exec
INSERT INTO agent_envs (agent_id, env, version_id, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (agent_id, env) DO UPDATE
SET version_id = EXCLUDED.version_id, updated_at = now();
