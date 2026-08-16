ALTER TABLE model_call_logs
    DROP COLUMN IF EXISTS experiment_variant,
    DROP COLUMN IF EXISTS experiment_id,
    DROP COLUMN IF EXISTS deployment_id,
    DROP COLUMN IF EXISTS model_profile_version_id,
    DROP COLUMN IF EXISTS prompt_version_id,
    DROP COLUMN IF EXISTS workspace_id;

DROP TABLE IF EXISTS conversation_runtime_configs;
DROP TABLE IF EXISTS prompt_rollout_events;
DROP INDEX IF EXISTS prompt_experiments_one_active;
DELETE FROM prompt_experiments a
USING prompt_experiments b
WHERE a.prompt_id=b.prompt_id AND a.env=b.env AND a.created_at < b.created_at;
ALTER TABLE prompt_experiments
    ADD CONSTRAINT prompt_experiments_prompt_id_env_key UNIQUE (prompt_id, env);

ALTER TABLE prompt_experiments
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS traffic_percent,
    DROP COLUMN IF EXISTS candidate_version_id,
    DROP COLUMN IF EXISTS baseline_version_id;

DROP TABLE IF EXISTS prompt_version_configs;
DROP TABLE IF EXISTS project_model_bindings;
DROP TABLE IF EXISTS model_profile_versions;
DROP TABLE IF EXISTS model_profiles;
DROP TABLE IF EXISTS model_deployments;

DROP INDEX IF EXISTS providers_workspace_name;
ALTER TABLE providers
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS api_key_ciphertext,
    DROP COLUMN IF EXISTS name,
    DROP COLUMN IF EXISTS workspace_id;
