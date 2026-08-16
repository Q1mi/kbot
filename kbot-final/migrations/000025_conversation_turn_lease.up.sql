-- 000025：同一会话同一时刻只允许一个 Chat/Resume turn 运行。

ALTER TABLE conversations
    ADD COLUMN turn_token UUID,
    ADD COLUMN turn_lease_until TIMESTAMPTZ,
    ADD COLUMN turn_revision BIGINT NOT NULL DEFAULT 0;

UPDATE conversations c
SET status = 'awaiting_approval'
WHERE EXISTS (
    SELECT 1 FROM approvals a
    WHERE a.conversation_id = c.id
      AND (a.status = 'pending' OR (a.status = 'approved' AND a.execution_status <> 'completed'))
);

CREATE INDEX conversations_turn_recovery
    ON conversations (turn_lease_until)
    WHERE status = 'running';
