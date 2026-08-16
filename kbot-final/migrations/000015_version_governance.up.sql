-- 000015_version_governance:让 Tool 发布门禁与具体不可变版本绑定。

ALTER TABLE tool_test_runs
    ADD COLUMN tool_version_id UUID REFERENCES tool_versions(id) ON DELETE CASCADE;

UPDATE tool_test_runs AS run
SET tool_version_id = (
    SELECT version.id
    FROM tool_versions AS version
    WHERE version.tool_id = run.tool_id
    ORDER BY version.version DESC
    LIMIT 1
);

ALTER TABLE tool_test_runs
    ALTER COLUMN tool_version_id SET NOT NULL;

CREATE INDEX tool_test_runs_version
    ON tool_test_runs (tool_version_id, created_at DESC);
