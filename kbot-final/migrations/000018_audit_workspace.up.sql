-- 000018_audit_workspace：审计日志增加强制工作空间维度。

ALTER TABLE audit_logs ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '';

UPDATE audit_logs a
SET workspace_id = c.workspace_id
FROM conversations c
WHERE a.resource_type = 'conversation'
  AND replace(a.resource_id, '-', '') = replace(c.id::text, '-', '');

CREATE INDEX audit_logs_workspace
    ON audit_logs (workspace_id, created_at DESC);
