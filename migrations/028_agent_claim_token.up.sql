-- 028: 队列所有权根治 —— 每次领取生成随机 claim_token，Complete/Fail 校验所有权。
-- 向后兼容：列可空，存量任务（token NULL）在租约过期重领时由 Claim 补发新 token。
ALTER TABLE pending_agent_tasks
    ADD COLUMN claim_token TEXT;
