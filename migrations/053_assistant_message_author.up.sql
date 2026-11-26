-- 053: 记录 assistant 消息发送者，共享会话可区分“我/其他成员”。
ALTER TABLE assistant_messages ADD COLUMN user_id UUID REFERENCES users(id);

-- 已有 user 消息回填为会话创建者（历史共享会话无法区分实际发送者）。
UPDATE assistant_messages m
SET user_id = s.user_id
FROM assistant_sessions s
WHERE m.session_id = s.id AND m.role = 'user' AND m.user_id IS NULL;

CREATE INDEX idx_assistant_messages_user ON assistant_messages(user_id);
