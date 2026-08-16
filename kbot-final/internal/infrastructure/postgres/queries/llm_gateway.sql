-- LLM Gateway:providers / model_aliases / routing_policies / model_call_logs

-- name: CreateProvider :one
INSERT INTO providers (id, kind, credential_ref, base_url, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListProviders :many
SELECT * FROM providers ORDER BY kind;

-- name: UpsertModelAlias :one
INSERT INTO model_aliases (id, alias, provider_id, model_name, capabilities, classification_max)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (alias) DO UPDATE
SET provider_id = EXCLUDED.provider_id, model_name = EXCLUDED.model_name,
    capabilities = EXCLUDED.capabilities, classification_max = EXCLUDED.classification_max
RETURNING *;

-- name: GetModelAlias :one
SELECT * FROM model_aliases WHERE alias = $1 LIMIT 1;

-- name: ListModelAliases :many
SELECT * FROM model_aliases ORDER BY alias;

-- name: CreateRoutingPolicy :one
INSERT INTO routing_policies (id, name, rules_json)
VALUES ($1, $2, $3)
RETURNING *;

-- name: InsertModelCallLog :exec
INSERT INTO model_call_logs (agent_id, user_id, provider_id, model, input_tokens, output_tokens, cached_tokens, cost, latency_ms, status, classification, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now());
