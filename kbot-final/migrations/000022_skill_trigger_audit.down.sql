DROP INDEX IF EXISTS skill_triggers_workspace_created;
ALTER TABLE skill_triggers DROP CONSTRAINT IF EXISTS skill_trigger_source_valid;
ALTER TABLE skill_triggers
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS skill_name,
    DROP COLUMN IF EXISTS workspace_id;
