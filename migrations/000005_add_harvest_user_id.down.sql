DROP INDEX IF EXISTS harvests_user_id_idx;
ALTER TABLE harvests DROP COLUMN IF EXISTS user_id;
