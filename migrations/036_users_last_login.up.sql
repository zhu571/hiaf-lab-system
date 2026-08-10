-- 036 users 最后登录追踪（S5 新 IP 登录告警）
-- auth/repository.go:57 查询 last_login_ip/last_login_at；auth/service.go:272 新 IP 登录触发告警并更新。
-- 本迁移于 2026-08-10 重建（原文件因并发工作流未提交被 reset --hard 误删，auth 代码已进 main 需此列）。
ALTER TABLE users
    ADD COLUMN last_login_ip VARCHAR(64),
    ADD COLUMN last_login_at TIMESTAMPTZ;
