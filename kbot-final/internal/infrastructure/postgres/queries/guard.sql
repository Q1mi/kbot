-- Guard:guard_rules / injection_logs / quota_ledger / approvals

-- name: ListGuardRules :many
SELECT * FROM guard_rules WHERE workspace_id = $1 ORDER BY kind, hook, id;

-- name: GetGuardRule :one
SELECT * FROM guard_rules WHERE id = $1 LIMIT 1;

-- name: CreateGuardRule :one
INSERT INTO guard_rules (id, kind, hook, pattern_or_model, action, enabled, workspace_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateGuardRule :one
UPDATE guard_rules
SET kind = $2, hook = $3, pattern_or_model = $4, action = $5, enabled = $6
WHERE id = $1
RETURNING *;

-- name: InsertInjectionLog :one
INSERT INTO injection_logs (id, conversation_id, sample, rule_id, severity, created_at)
VALUES ($1, $2, $3, $4, $5, now())
RETURNING *;

-- name: ListInjectionLogs :many
SELECT * FROM injection_logs WHERE conversation_id = $1 ORDER BY created_at DESC;

-- name: SetQuotaLimit :one
INSERT INTO quota_ledger (dimension, period, used, lim)
VALUES ($1, $2, 0, $3)
ON CONFLICT (dimension, period) DO UPDATE SET lim = EXCLUDED.lim
RETURNING *;

-- name: ConsumeQuota :one
UPDATE quota_ledger
SET used = used + sqlc.arg(amount)::bigint
WHERE dimension = sqlc.arg(dimension) AND period = sqlc.arg(period)
  AND (lim = 0 OR used + sqlc.arg(amount)::bigint <= lim)
RETURNING *;

-- name: GetQuota :one
SELECT * FROM quota_ledger WHERE dimension = $1 AND period = $2 LIMIT 1;

-- name: ListWorkspaceQuotas :many
SELECT * FROM quota_ledger
WHERE dimension LIKE 'workspace:' || sqlc.arg(workspace_id)::text || ':%'
  AND period = sqlc.arg(period)
ORDER BY dimension;

-- name: CreateApproval :one
INSERT INTO approvals (id, workspace_id, conversation_id, action, payload, status, created_at)
VALUES ($1, $2, $3, $4, $5, 'pending', now())
RETURNING *;

-- name: GetApproval :one
SELECT * FROM approvals WHERE id = $1 LIMIT 1;

-- name: ResolvePendingApproval :one
UPDATE approvals
SET status = sqlc.arg(status), approver_id = sqlc.arg(approver_id), resolved_at = now()
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
  AND status = 'pending'
RETURNING *;

-- name: ListPendingApprovals :many
SELECT * FROM approvals
WHERE workspace_id = $1 AND status = 'pending'
ORDER BY created_at DESC;

-- name: ListApprovalsByConversation :many
SELECT * FROM approvals WHERE conversation_id = $1 ORDER BY created_at;

-- name: BeginApprovalExecution :one
UPDATE approvals
SET execution_status = 'executing', execution_started_at = now(), execution_completed_at = NULL,
    execution_error = '', execution_lease_until = now() + interval '2 minutes',
    execution_attempts = execution_attempts + 1, execution_token = sqlc.arg(execution_token)
WHERE id = sqlc.arg(id)
  AND conversation_id = sqlc.arg(conversation_id)
  AND status = 'approved'
  AND execution_attempts < 5
  AND (
      execution_status = 'not_started'
      OR execution_status = 'failed'
      OR (execution_status = 'executing' AND execution_lease_until < now())
  )
RETURNING *;

-- name: RenewApprovalExecution :execrows
UPDATE approvals
SET execution_lease_until = now() + interval '2 minutes'
WHERE id = sqlc.arg(id)
  AND execution_status = 'executing'
  AND execution_token = sqlc.arg(execution_token);

-- name: CompleteApprovalExecution :execrows
UPDATE approvals
SET execution_status = 'completed', execution_completed_at = now(), execution_error = '',
    execution_lease_until = NULL, execution_token = NULL
WHERE id = sqlc.arg(id) AND execution_status = 'executing'
  AND execution_token = sqlc.arg(execution_token);

-- name: FailApprovalExecution :execrows
UPDATE approvals
SET execution_status = 'failed', execution_completed_at = now(), execution_error = sqlc.arg(execution_error),
    execution_lease_until = NULL, execution_token = NULL
WHERE id = sqlc.arg(id) AND execution_status = 'executing'
  AND execution_token = sqlc.arg(execution_token);

-- name: ListReadyApprovalResumes :many
SELECT * FROM approvals
WHERE status = 'approved'
  AND execution_attempts < 5
  AND (
      execution_status = 'not_started'
      OR execution_status = 'failed'
      OR (execution_status = 'executing' AND execution_lease_until < now())
  )
ORDER BY resolved_at ASC
LIMIT $1;
