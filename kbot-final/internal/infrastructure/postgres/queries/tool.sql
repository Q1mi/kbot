-- Tool:tools / tool_versions / tool_test_runs

-- name: GetTool :one
SELECT * FROM tools WHERE id = $1 LIMIT 1;

-- name: ListTools :many
SELECT * FROM tools WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateTool :one
INSERT INTO tools (id, workspace_id, name, description, source_type, sensitive, classification_max, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())
RETURNING *;

-- name: CreateToolWithVersion :exec
WITH inserted_tool AS (
  INSERT INTO tools (id, workspace_id, name, description, source_type, sensitive, classification_max, created_by, created_at, updated_at)
  VALUES (sqlc.arg(tool_id), sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(source_type),
          sqlc.arg(sensitive), sqlc.arg(classification_max), sqlc.arg(tool_created_by), now(), now())
  RETURNING id
)
INSERT INTO tool_versions
  (id, tool_id, version, schema_json, endpoint_config, auth_config, auth_config_encrypted, retry_policy, status, created_by, created_at)
SELECT sqlc.arg(version_id), id, 1, sqlc.arg(schema_json), sqlc.arg(endpoint_config), sqlc.arg(auth_config),
       sqlc.arg(auth_config_encrypted), sqlc.arg(retry_policy), sqlc.arg(version_status), sqlc.arg(version_created_by), now()
FROM inserted_tool;

-- name: GetToolVersion :one
SELECT * FROM tool_versions WHERE id = $1 LIMIT 1;

-- name: GetToolCurrentVersion :one
-- 「当前版本」= 版本号最大者(memory 版 CreateToolVersion 把最新创建设为 current,版本号单调递增故等价)。
SELECT * FROM tool_versions WHERE tool_id = $1 ORDER BY version DESC LIMIT 1;

-- name: ListToolVersions :many
SELECT * FROM tool_versions WHERE tool_id = $1 ORDER BY version DESC;

-- name: GetToolLatestPublishedVersion :one
SELECT * FROM tool_versions WHERE tool_id = $1 AND status = 'published' ORDER BY version DESC LIMIT 1;

-- name: CreateToolVersion :one
INSERT INTO tool_versions (id, tool_id, version, schema_json, endpoint_config, auth_config, auth_config_encrypted, retry_policy, status, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
RETURNING *;

-- name: ListLegacyToolAuthVersions :many
SELECT * FROM tool_versions
WHERE auth_config_encrypted IS NULL
  AND btrim(auth_config) NOT IN ('', '{}');

-- name: EncryptToolVersionAuth :exec
UPDATE tool_versions
SET auth_config = '{}', auth_config_encrypted = $2
WHERE id = $1 AND auth_config_encrypted IS NULL;

-- name: CreateToolInvocation :exec
INSERT INTO tool_invocations (
    id, workspace_id, conversation_id, tool_call_id, tool_version_id,
    args, result, status, latency_ms, error, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now());

-- name: CompleteToolInvocation :execrows
UPDATE tool_invocations
SET result = $2, status = $3, latency_ms = $4, error = $5
WHERE id = $1 AND status = 'running';

-- name: CreateSandboxExecution :exec
INSERT INTO sandbox_executions (
    id, workspace_id, conversation_id, invocation_id, tool_version_id, tool_call_id,
    execution_id, language, container_id, exit_code, stdout, stderr,
    resource_usage, duration_ms, timed_out, output_truncated, status, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        '{}', $13, $14, $15, $16, now());

-- name: UpdateToolVersionStatus :exec
UPDATE tool_versions SET status = $2 WHERE id = $1;

-- name: CreateToolTestRun :one
INSERT INTO tool_test_runs (id, tool_id, tool_version_id, input, output, status, latency_ms, error, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
RETURNING *;

-- name: GetToolLastSuccessfulTestRun :one
SELECT * FROM tool_test_runs WHERE tool_id = $1 AND status = 'success' ORDER BY created_at DESC LIMIT 1;

-- name: GetToolLastSuccessfulTestRunForVersion :one
SELECT * FROM tool_test_runs WHERE tool_version_id = $1 AND status = 'success' ORDER BY created_at DESC LIMIT 1;
