-- Conversation:conversations / messages(agent.Store 的会话部分)

-- name: GetConversation :one
SELECT * FROM conversations WHERE id = $1 LIMIT 1;

-- name: ListConversations :many
SELECT * FROM conversations
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id)
  AND (sqlc.arg(agent_id)::text = '' OR agent_id = sqlc.arg(agent_id))
ORDER BY updated_at DESC, started_at DESC
LIMIT sqlc.arg(page_limit)::int OFFSET sqlc.arg(page_offset)::int;

-- name: CreateConversation :one
INSERT INTO conversations (id, agent_id, agent_version_id, user_id, workspace_id, classification, status, started_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
RETURNING *;

-- name: CreateMessage :one
INSERT INTO messages (id, conversation_id, role, content, tool_calls, tool_call_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
RETURNING *;

-- name: ListMessages :many
SELECT * FROM messages WHERE conversation_id = $1 ORDER BY created_at;

-- name: CreateCheckpoint :exec
INSERT INTO checkpoints (id, conversation_id, approval_id, state_snapshot, created_at)
VALUES ($1, $2, $3, $4, now());

-- name: GetCheckpointForApproval :one
SELECT * FROM checkpoints WHERE approval_id = $1 AND conversation_id = $2 LIMIT 1;
