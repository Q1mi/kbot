-- Team: teams / team_versions / team_envs

-- name: GetTeam :one
SELECT * FROM teams WHERE id = $1 LIMIT 1;

-- name: ListTeams :many
SELECT * FROM teams WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateTeam :one
INSERT INTO teams (id, workspace_id, name, mode, created_at)
VALUES ($1, $2, $3, $4, now())
RETURNING *;

-- name: CreateTeamWithVersion :exec
WITH inserted_team AS (
  INSERT INTO teams (id, workspace_id, name, mode, created_at)
  VALUES (sqlc.arg(team_id), sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(mode), now())
  RETURNING id
), inserted_version AS (
  INSERT INTO team_versions (id, team_id, version, snapshot_json, created_at)
  SELECT sqlc.arg(version_id), id, 1, sqlc.arg(snapshot_json), now() FROM inserted_team
  RETURNING id, team_id
)
INSERT INTO team_envs (team_id, env, version_id)
SELECT team_id, 'dev', id FROM inserted_version;

-- name: GetTeamVersion :one
SELECT * FROM team_versions WHERE id = $1 LIMIT 1;

-- name: ListTeamVersions :many
SELECT * FROM team_versions WHERE team_id = $1 ORDER BY version DESC;

-- name: CreateTeamVersion :one
INSERT INTO team_versions (id, team_id, version, snapshot_json, created_at)
VALUES ($1, $2, $3, $4, now())
RETURNING *;

-- name: CreateTeamVersionAndBindDev :exec
WITH inserted_version AS (
  INSERT INTO team_versions (id, team_id, version, snapshot_json, created_at)
  VALUES (sqlc.arg(version_id), sqlc.arg(team_id), sqlc.arg(version), sqlc.arg(snapshot_json), now())
  RETURNING id, team_id
)
INSERT INTO team_envs (team_id, env, version_id)
SELECT team_id, 'dev', id FROM inserted_version
ON CONFLICT (team_id, env) DO UPDATE SET version_id=EXCLUDED.version_id;

-- name: GetTeamCurrentVersion :one
SELECT tv.* FROM team_versions tv
JOIN team_envs te ON te.version_id = tv.id
WHERE te.team_id = $1 AND te.env = $2
LIMIT 1;

-- name: UpsertTeamEnv :exec
INSERT INTO team_envs (team_id, env, version_id)
VALUES ($1, $2, $3)
ON CONFLICT (team_id, env) DO UPDATE SET version_id = EXCLUDED.version_id;
