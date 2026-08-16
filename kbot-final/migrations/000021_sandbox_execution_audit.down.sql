DROP INDEX IF EXISTS sandbox_executions_workspace_created;
DROP INDEX IF EXISTS sandbox_executions_external_id;
ALTER TABLE sandbox_executions DROP CONSTRAINT IF EXISTS sandbox_execution_status_valid;
ALTER TABLE sandbox_executions
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS output_truncated,
    DROP COLUMN IF EXISTS timed_out,
    DROP COLUMN IF EXISTS duration_ms,
    DROP COLUMN IF EXISTS language,
    DROP COLUMN IF EXISTS execution_id,
    DROP COLUMN IF EXISTS tool_call_id,
    DROP COLUMN IF EXISTS tool_version_id,
    DROP COLUMN IF EXISTS invocation_id,
    DROP COLUMN IF EXISTS conversation_id,
    DROP COLUMN IF EXISTS workspace_id;
