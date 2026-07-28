-- 用户语言偏好：前端 i18n 使用，登录/刷新时随 user 信息返回。

ALTER TABLE users ADD COLUMN language VARCHAR(8) NOT NULL DEFAULT 'zh'
    CHECK (language IN ('zh', 'en'));
