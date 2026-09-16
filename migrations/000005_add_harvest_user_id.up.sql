ALTER TABLE harvests ADD COLUMN user_id UUID;
CREATE INDEX harvests_user_id_idx ON harvests (user_id);
