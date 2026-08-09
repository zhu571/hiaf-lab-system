-- 034 ask_history 快照保留策略：rows 列改可空（P2-3，方案 §2.11 定死方案）。
-- 90 天前的快照由 ask 服务内置每日维护任务置 NULL（不做 DELETE、不做 admin 端点），
-- 保留历史列表可用性；置 NULL 后由 autovacuum 自然回收空间。
-- 只 DROP NOT NULL，无数据重写，可安全回滚。
ALTER TABLE ask_history ALTER COLUMN rows DROP NOT NULL;
