-- 000020_tool_invocation_audit：持久化每次 Tool 调用并按工作空间隔离。

ALTER TABLE tool_invocations
    ADD COLUMN workspace_id TEXT,
    ADD COLUMN tool_call_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN status TEXT NOT NULL DEFAULT 'success';

UPDATE tool_invocations i
SET workspace_id = c.workspace_id
FROM conversations c
WHERE c.id = i.conversation_id AND i.workspace_id IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tool_invocations WHERE workspace_id IS NULL) THEN
        RAISE EXCEPTION 'cannot migrate tool invocations without a valid conversation workspace';
    END IF;
END $$;

ALTER TABLE tool_invocations ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE tool_invocations
    ADD CONSTRAINT tool_invocations_status_valid CHECK (status IN ('running', 'success', 'error', 'denied'));

CREATE UNIQUE INDEX tool_invocations_call_once
    ON tool_invocations (conversation_id, tool_call_id)
    WHERE tool_call_id <> '';
CREATE INDEX tool_invocations_workspace_created
    ON tool_invocations (workspace_id, created_at DESC);
