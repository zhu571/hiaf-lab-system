-- 用户停用：disabled=true 的账户禁止登录、刷新 token，且已有 access token 立即失效。

ALTER TABLE users ADD COLUMN disabled BOOLEAN NOT NULL DEFAULT false;
