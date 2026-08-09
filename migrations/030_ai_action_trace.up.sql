-- 030: AI 动作完整审计链（C8）—— 任务级原始输入快照 + 执行产物反链。
-- 快照由 py-agent worker complete 时回传（raw_text_snapshot/report_date），
-- raw_text_sha256 由 Go 侧计算落库；agent 模块不读取 daily_reports 表。
ALTER TABLE pending_agent_tasks
    ADD COLUMN raw_text_snapshot TEXT,
    ADD COLUMN raw_text_sha256 CHAR(64),
    ADD COLUMN report_date DATE;

-- 执行产物反链：issue/experience 指向来源候选（溯源 join 的根）。
-- 语义：仅 FK 约束保证引用完整性；仓库层禁止跨模块查询/join
-- （agent_candidate_actions 归 agent 模块，issues/experiences 不直接查它，
--  需要时经 agent 模块 service/HTTP 获取，符合 AGENTS.md 模块单向依赖）。
ALTER TABLE issues
    ADD COLUMN candidate_id UUID REFERENCES agent_candidate_actions(id);
ALTER TABLE experiences
    ADD COLUMN candidate_id UUID REFERENCES agent_candidate_actions(id);
