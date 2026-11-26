DROP INDEX IF EXISTS idx_assistant_messages_user;
ALTER TABLE assistant_messages DROP COLUMN IF EXISTS user_id;
