-- 000021_sandbox_execution_audit：把 Runner 结构化结果关联到 Tool Invocation。

ALTER TABLE sandbox_executions
    ADD COLUMN workspace_id TEXT,
    ADD COLUMN conversation_id UUID,
    ADD COLUMN invocation_id UUID REFERENCES tool_invocations(id) ON DELETE SET NULL,
    ADD COLUMN tool_version_id UUID REFERENCES tool_versions(id) ON DELETE SET NULL,
    ADD COLUMN tool_call_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN execution_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN language TEXT NOT NULL DEFAULT '',
    ADD COLUMN duration_ms BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN timed_out BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN output_truncated BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN status TEXT NOT NULL DEFAULT 'success';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM sandbox_executions WHERE workspace_id IS NULL) THEN
        RAISE EXCEPTION 'cannot migrate legacy sandbox executions without workspace ownership';
    END IF;
END $$;

ALTER TABLE sandbox_executions ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE sandbox_executions
    ADD CONSTRAINT sandbox_execution_status_valid CHECK (status IN ('success', 'error', 'timeout'));

CREATE UNIQUE INDEX sandbox_executions_external_id
    ON sandbox_executions (execution_id) WHERE execution_id <> '';
CREATE INDEX sandbox_executions_workspace_created
    ON sandbox_executions (workspace_id, created_at DESC);
