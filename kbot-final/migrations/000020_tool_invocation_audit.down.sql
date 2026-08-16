DROP INDEX IF EXISTS tool_invocations_workspace_created;
DROP INDEX IF EXISTS tool_invocations_call_once;
ALTER TABLE tool_invocations DROP CONSTRAINT IF EXISTS tool_invocations_status_valid;
ALTER TABLE tool_invocations
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS tool_call_id,
    DROP COLUMN IF EXISTS workspace_id;
