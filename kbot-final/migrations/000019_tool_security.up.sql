-- 000019_tool_security：工具名称唯一、凭证密文存储。

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM tools GROUP BY workspace_id, name HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate tool names must be resolved before migration 19';
    END IF;
END $$;

DROP INDEX IF EXISTS tools_workspace_name;
CREATE UNIQUE INDEX tools_workspace_name_unique ON tools (workspace_id, name);

ALTER TABLE tool_versions ADD COLUMN auth_config_encrypted BYTEA;
