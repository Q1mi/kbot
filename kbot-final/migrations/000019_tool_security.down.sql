ALTER TABLE tool_versions DROP COLUMN IF EXISTS auth_config_encrypted;
DROP INDEX IF EXISTS tools_workspace_name_unique;
CREATE INDEX tools_workspace_name ON tools (workspace_id, name);
