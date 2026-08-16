DROP INDEX IF EXISTS checkpoints_approval;
ALTER TABLE checkpoints DROP CONSTRAINT IF EXISTS checkpoints_approval_fk;
ALTER TABLE checkpoints DROP COLUMN IF EXISTS approval_id;
