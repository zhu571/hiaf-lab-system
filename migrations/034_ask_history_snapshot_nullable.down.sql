-- 034 down：先补 '[]' 再恢复 NOT NULL（幂等可回滚）。
UPDATE ask_history SET rows = '[]' WHERE rows IS NULL;
ALTER TABLE ask_history ALTER COLUMN rows SET NOT NULL;
