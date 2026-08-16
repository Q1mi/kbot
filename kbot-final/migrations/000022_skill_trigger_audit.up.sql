-- 000022_skill_trigger_audit：技能触发记录增加租户和触发来源。

ALTER TABLE skill_triggers
    ADD COLUMN workspace_id TEXT,
    ADD COLUMN skill_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN source TEXT NOT NULL DEFAULT 'model';

UPDATE skill_triggers st
SET workspace_id = c.workspace_id
FROM conversations c
WHERE c.id = st.conversation_id AND st.workspace_id IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM skill_triggers WHERE workspace_id IS NULL) THEN
        RAISE EXCEPTION 'cannot migrate skill triggers without workspace ownership';
    END IF;
END $$;

ALTER TABLE skill_triggers ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE skill_triggers
    ADD CONSTRAINT skill_trigger_source_valid CHECK (source IN ('model', 'user'));
CREATE INDEX skill_triggers_workspace_created
    ON skill_triggers (workspace_id, triggered_at DESC);
