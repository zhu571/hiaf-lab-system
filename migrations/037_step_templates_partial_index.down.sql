-- 037 down：回滚为全量唯一索引（等价 024 原状；此时软删行若与活动行同序会冲突，
-- 属回滚前的数据状态问题，由运维按实际情况处理）。
DROP INDEX uq_template_item_order;
CREATE UNIQUE INDEX uq_template_item_order ON step_template_items(template_id, step_order);
