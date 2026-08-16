-- Jobs:jobs / job_schedules / dead_letters

-- name: CreateJob :one
INSERT INTO jobs (id, workspace_id, type, payload, status, scheduled_at, idempotency_key)
VALUES ($1, $2, $3, $4, 'pending', $5, $6)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1 LIMIT 1;

-- name: ListPendingJobs :many
SELECT * FROM jobs WHERE status = 'pending' ORDER BY scheduled_at NULLS FIRST LIMIT $1;

-- name: UpdateJobStatus :exec
UPDATE jobs SET status = $2, attempts = $3, started_at = $4, finished_at = $5, error = $6 WHERE id = $1;

-- name: CreateJobSchedule :one
INSERT INTO job_schedules (id, type, payload, cron, next_run_at, enabled)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListEnabledSchedules :many
SELECT * FROM job_schedules WHERE enabled = true;

-- name: CreateDeadLetter :one
INSERT INTO dead_letters (id, job_id, payload, error, dlq_at)
VALUES ($1, $2, $3, $4, now())
RETURNING *;
