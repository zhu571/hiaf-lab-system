-- 037 step_template_items 唯一约束改为部分索引：软删的旧条目不占 step_order 槽位。
-- 背景（本轮覆盖测试发现的真实 bug）：024 建的 uq_template_item_order 是全量唯一索引，
-- ReplaceItems 先软删旧条目（deleted_at=now()）再按 1..N 规范化插入新条目，旧行仍占
-- (template_id, step_order)，新序号一旦与旧序号重叠即 23505 → PATCH /items 必 500。
-- 修复：唯一性只约束「未删除」条目（deleted_at IS NULL），语义与 GetByID/getItems
-- 的软删过滤一致；旧索引无 WHERE 条件，直接 DROP 换部分索引，无数据重写。
DROP INDEX uq_template_item_order;
CREATE UNIQUE INDEX uq_template_item_order
    ON step_template_items(template_id, step_order) WHERE deleted_at IS NULL;
