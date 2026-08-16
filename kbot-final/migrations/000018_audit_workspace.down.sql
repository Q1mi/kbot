DROP INDEX IF EXISTS audit_logs_workspace;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS workspace_id;
