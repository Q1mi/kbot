-- Prompt:prompts / prompt_versions / prompt_envs / prompt_experiments

-- name: GetPrompt :one
SELECT * FROM prompts WHERE id = $1 LIMIT 1;

-- name: ListPrompts :many
SELECT * FROM prompts WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreatePrompt :one
INSERT INTO prompts (id, workspace_id, name, category, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now())
RETURNING *;

-- name: GetPromptVersion :one
SELECT * FROM prompt_versions WHERE id = $1 LIMIT 1;

-- name: GetPromptVersionByNumber :one
SELECT * FROM prompt_versions WHERE prompt_id = $1 AND version = $2 LIMIT 1;

-- name: ListPromptVersions :many
SELECT * FROM prompt_versions WHERE prompt_id = $1 ORDER BY version;

-- name: CreatePromptVersion :one
INSERT INTO prompt_versions (id, prompt_id, version, template, variables_schema, hash, token_estimate, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
RETURNING *;

-- name: GetPromptEnv :one
SELECT version_id FROM prompt_envs WHERE prompt_id = $1 AND env = $2 LIMIT 1;

-- name: UpsertPromptEnv :exec
INSERT INTO prompt_envs (prompt_id, env, version_id, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (prompt_id, env) DO UPDATE
SET version_id = EXCLUDED.version_id, updated_at = now();

-- name: GetActiveExperiment :one
SELECT * FROM prompt_experiments WHERE prompt_id = $1 AND env = $2 LIMIT 1;

-- name: UpsertExperiment :exec
INSERT INTO prompt_experiments (id, prompt_id, env, variants, status, started_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (prompt_id, env) DO UPDATE
SET variants = EXCLUDED.variants, status = EXCLUDED.status, started_at = EXCLUDED.started_at;
