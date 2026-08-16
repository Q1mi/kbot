-- 000016_workspace_rbac：启用现有 IAM 表，约束全局角色和工作空间角色。

ALTER TABLE users
    ADD CONSTRAINT users_role_valid
    CHECK (role IN ('admin', 'member'));

ALTER TABLE workspace_members
    ADD CONSTRAINT workspace_members_role_valid
    CHECK (role IN ('owner', 'admin', 'editor', 'member', 'viewer'));

CREATE INDEX workspace_members_workspace_role
    ON workspace_members (workspace_id, role);
