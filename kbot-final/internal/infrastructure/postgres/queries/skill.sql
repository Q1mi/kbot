-- Skill:skills / skill_versions / skill_subscriptions

-- name: GetSkill :one
SELECT * FROM skills WHERE id = $1 LIMIT 1;

-- name: ListSkills :many
SELECT * FROM skills WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateSkill :one
INSERT INTO skills (id, workspace_id, name, category, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now())
RETURNING *;

-- name: CreateSkillWithVersion :exec
WITH inserted_skill AS (
  INSERT INTO skills (id, workspace_id, name, category, created_by, created_at, updated_at)
  VALUES (sqlc.arg(skill_id), sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(category), sqlc.arg(skill_created_by), now(), now())
  RETURNING id
)
INSERT INTO skill_versions (id, skill_id, version, frontmatter_json, body_md, status, created_by, created_at)
SELECT sqlc.arg(version_id), id, 1, sqlc.arg(frontmatter_json), sqlc.arg(body_md), sqlc.arg(version_status),
       sqlc.arg(version_created_by), now()
FROM inserted_skill;

-- name: GetSkillVersion :one
SELECT * FROM skill_versions WHERE id = $1 LIMIT 1;

-- name: ListSkillVersions :many
SELECT * FROM skill_versions WHERE skill_id = $1 ORDER BY version;

-- name: CreateSkillVersion :one
INSERT INTO skill_versions (id, skill_id, version, frontmatter_json, body_md, status, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
RETURNING *;

-- name: UpdateSkillVersionStatus :exec
UPDATE skill_versions SET status = $2 WHERE id = $1;

-- name: CreateSkillSubscription :exec
INSERT INTO skill_subscriptions (skill_id, version_id, agent_id, workspace_id)
VALUES ($1, $2, $3, $4);

-- name: ListSubscriptionsForAgent :many
SELECT * FROM skill_subscriptions WHERE agent_id = $1 ORDER BY id;
