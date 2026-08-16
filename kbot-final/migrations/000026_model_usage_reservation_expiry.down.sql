DROP INDEX IF EXISTS project_model_usage_expiry;
ALTER TABLE project_model_usage_reservations DROP COLUMN IF EXISTS expires_at;
