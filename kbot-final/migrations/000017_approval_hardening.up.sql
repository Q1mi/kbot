-- 000017_approval_hardening：审批租户隔离、原子决议和恢复执行状态。

ALTER TABLE approvals ADD COLUMN workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE;

UPDATE approvals a
SET workspace_id = c.workspace_id
FROM conversations c
WHERE c.id = a.conversation_id AND a.workspace_id IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM approvals WHERE workspace_id IS NULL) THEN
        RAISE EXCEPTION 'cannot migrate approvals without a valid conversation workspace';
    END IF;
END $$;

ALTER TABLE approvals ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE approvals
    ADD CONSTRAINT approvals_status_valid
    CHECK (status IN ('pending', 'approved', 'rejected'));

ALTER TABLE approvals
    ADD COLUMN execution_status TEXT NOT NULL DEFAULT 'not_started',
    ADD COLUMN execution_started_at TIMESTAMPTZ,
    ADD COLUMN execution_completed_at TIMESTAMPTZ,
    ADD COLUMN execution_error TEXT NOT NULL DEFAULT '';

ALTER TABLE approvals
    ADD CONSTRAINT approvals_execution_status_valid
    CHECK (execution_status IN ('not_started', 'executing', 'completed', 'failed'));

CREATE INDEX approvals_workspace_status
    ON approvals (workspace_id, status, created_at DESC);
