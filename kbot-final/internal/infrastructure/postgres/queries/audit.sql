-- Audit:audit_logs(分区父表,查询透明走分区)

-- name: InsertAuditLog :exec
INSERT INTO audit_logs (workspace_id, actor, action, resource_type, resource_id, before_json, after_json, ip, ua, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- QueryAuditLogs:可选过滤(actor / resource_type / conversation),倒序最新优先。
-- limit 为 0 时 NULLIF→NULL,即不限制(对齐 memory 版 Limit<=0 返回全部)。
-- name: QueryAuditLogs :many
SELECT * FROM audit_logs
WHERE workspace_id = sqlc.arg(workspace_id)::text
  AND (sqlc.arg(actor)::text = '' OR actor = sqlc.arg(actor)::text)
  AND (sqlc.arg(resource_type)::text = '' OR resource_type = sqlc.arg(resource_type)::text)
  AND (sqlc.arg(conversation_id)::text = '' OR (resource_type = 'conversation' AND resource_id = sqlc.arg(conversation_id)::text))
ORDER BY created_at DESC
LIMIT NULLIF(sqlc.arg(lim)::int, 0);

-- name: InsertSkillTrigger :exec
INSERT INTO skill_triggers (
    id, workspace_id, conversation_id, skill_version_id, skill_name, source, success, triggered_at
)
VALUES ($1, $2, $3, $4, $5, $6, true, now());
