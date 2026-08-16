DROP INDEX IF EXISTS approvals_ready_resume;
ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_execution_attempts_valid;
ALTER TABLE approvals
    DROP COLUMN IF EXISTS execution_token,
    DROP COLUMN IF EXISTS execution_attempts,
    DROP COLUMN IF EXISTS execution_lease_until;
