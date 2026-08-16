-- 000024：审批恢复执行使用带 fencing token 的租约，可回收崩溃和临时失败。

ALTER TABLE approvals
    ADD COLUMN execution_lease_until TIMESTAMPTZ,
    ADD COLUMN execution_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN execution_token UUID;

ALTER TABLE approvals
    ADD CONSTRAINT approvals_execution_attempts_valid CHECK (execution_attempts >= 0);

CREATE INDEX approvals_ready_resume
    ON approvals (resolved_at)
    WHERE status = 'approved' AND execution_status IN ('not_started', 'executing', 'failed');
