DROP INDEX IF EXISTS approvals_workspace_status;
ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_execution_status_valid;
ALTER TABLE approvals
    DROP COLUMN IF EXISTS execution_error,
    DROP COLUMN IF EXISTS execution_completed_at,
    DROP COLUMN IF EXISTS execution_started_at,
    DROP COLUMN IF EXISTS execution_status;
ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_status_valid;
ALTER TABLE approvals DROP COLUMN IF EXISTS workspace_id;
