-- 009_test_data.up.sql
--
-- 本迁移已改为空操作（R7，2026-08-16）：
--   * 原内容先全量 DELETE 业务表再插入 5 个测试用户（含已知密码 Test1234! 的 admin），
--     随迁移链在全新部署自动执行等于「新环境自带弱口令管理员」；误用于已有库则是全量清库灾难。
--   * 版本号 009 保留（golang-migrate 按 schema_migrations 记录判定已应用，不会重跑）；
--     历史环境不受影响，全新环境执行本文件不再产生任何数据。
--   * 原测试数据集（5 用户 / 3 项目 / 10 日报 / 22 日志 / 8 issue / 5 经验 / 5 审计行）
--     已原样移至 go-server/cmd/seed-testdata/seed.sql，仅在开发/测试环境显式执行：
--         cd go-server && go run ./cmd/seed-testdata
--     （scripts/test-go.sh、scripts/test-e2e.sh 与 CI go-test job 已在迁移后自动执行该 seed。）
--
-- 注意：audit_log 的插入依赖 029 的 BEFORE INSERT hash 链触发器自动接链，
-- seed 在全量迁移之后运行，链完整性不受影响。

-- 空操作：无任何语句。
SELECT 1;
