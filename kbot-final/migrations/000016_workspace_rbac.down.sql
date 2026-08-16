DROP INDEX IF EXISTS workspace_members_workspace_role;
ALTER TABLE workspace_members DROP CONSTRAINT IF EXISTS workspace_members_role_valid;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_valid;
