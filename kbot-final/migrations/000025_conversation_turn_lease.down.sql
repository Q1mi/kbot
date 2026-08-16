DROP INDEX IF EXISTS conversations_turn_recovery;
ALTER TABLE conversations
    DROP COLUMN IF EXISTS turn_revision,
    DROP COLUMN IF EXISTS turn_lease_until,
    DROP COLUMN IF EXISTS turn_token;
