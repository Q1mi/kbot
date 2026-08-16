-- Eval:eval_datasets / eval_cases / eval_runs / eval_scores

-- name: GetEvalDataset :one
SELECT * FROM eval_datasets WHERE id = $1 LIMIT 1;

-- name: ListEvalDatasets :many
SELECT * FROM eval_datasets WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateEvalDataset :one
INSERT INTO eval_datasets (id, workspace_id, name, target_kind, created_at)
VALUES ($1, $2, $3, $4, now())
RETURNING *;

-- name: AddEvalCase :one
INSERT INTO eval_cases (id, dataset_id, input, expected, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, now())
RETURNING *;

-- name: ListEvalCases :many
SELECT * FROM eval_cases WHERE dataset_id = $1 ORDER BY created_at;

-- name: CreateEvalRun :one
INSERT INTO eval_runs (id, dataset_id, target_id, judge_id, status, pass_rate, threshold, created_at, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8)
RETURNING *;

-- name: AddEvalScore :exec
INSERT INTO eval_scores (run_id, case_id, dimension, score, reason)
VALUES ($1, $2, $3, $4, $5);

-- name: GetEvalRun :one
SELECT * FROM eval_runs WHERE id = $1 LIMIT 1;

-- name: ListEvalRuns :many
SELECT * FROM eval_runs WHERE dataset_id = $1 ORDER BY created_at DESC;

-- name: ListEvalScores :many
SELECT * FROM eval_scores WHERE run_id = $1 ORDER BY case_id, dimension;
