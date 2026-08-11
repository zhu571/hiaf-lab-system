#!/bin/bash
set -euo pipefail

# ============================================================
# lab-restore-check.sh — 数据库恢复演练（10h 优化 A 项）
#
# 用途：把最近一份备份（或 $1 指定的备份文件）pg_restore 到临时库 lab_restore_check，
#       校验恢复结果（表数与生产一致、迁移版本号含 001-037、daily_reports 最新日期非空），
#       输出「通过/失败」，无论成败都 drop 临时库（trap 兜底）。
#       只动临时库，不碰生产库任何数据，可安全在 gascell 部署机上手动执行。
#
# 先例对照：
#   - 容器内执行 psql/pg_restore：沿用 .hermes/update.sh:175-183 的
#     `docker compose -f "$COMPOSE_FILE" exec -T postgres <命令>` 方式，不用宿主机 psql
#   - compose 服务名：deploy/docker-compose.yml:2 `postgres:`（container_name lab-postgres）
#
# 用法（可在备份目录外的任意位置执行）：
#   bash deploy/scripts/restore-check.sh             # 取 /opt/lab-backups 下最新备份
#   bash deploy/scripts/restore-check.sh /path/to/lab-20260811.dump
#
# 可用 env 覆盖（与 backup.sh 同款）：
#   BACKUP_DIR               备份目录（默认 /opt/lab-backups）
#   BACKUP_COMPOSE_FILE      docker-compose.yml 绝对路径（默认按脚本位置推导仓库路径）
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

COMPOSE_FILE="${BACKUP_COMPOSE_FILE:-$REPO_ROOT/deploy/docker-compose.yml}"
BACKUP_DIR="${BACKUP_DIR:-/opt/lab-backups}"
TEMP_DB="lab_restore_check"
# 迁移 001-037 共 37 个版本（golang-migrate 顺序执行，见 migrations/）
EXPECTED_MIGRATIONS=37
# 表数核实：migrations/*.up.sql 共 35 个 CREATE TABLE + golang-migrate 自建 schema_migrations 表 ≈ 36 张。
# 主判据是临时库与生产表数严格相等；MIN_TABLES=30 只是防「两边都空」误判的兜底下限。
MIN_TABLES=30

RESTORE_FILE="${1:-}"
if [ -z "$RESTORE_FILE" ]; then
  RESTORE_FILE="$(ls -t "$BACKUP_DIR"/lab-*.dump 2>/dev/null | head -1 || true)"
fi
if [ -z "$RESTORE_FILE" ] || [ ! -f "$RESTORE_FILE" ]; then
  echo "失败：未找到可用的备份文件（$BACKUP_DIR/lab-*.dump 或 ${1:-<未传参>}）"
  exit 1
fi

# 容器内执行 psql（$1 库名 $2 SQL），-tAc 只输出查询结果；查询失败给出明确诊断
# （避免 set -e 静默退出；失败时退出码非 0，EXIT trap 仍会清理临时库）
psql_exec() {
  docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U lab -d "$1" -tAc "$2" || {
    echo "失败：psql 查询失败（postgres 未运行或容器不可达）" >&2
    exit 1
  }
}

# 无论成败都清理临时库（trap 兜底，set -e 提前退出也会走到这里）
cleanup() {
  set +e
  echo "清理临时库 $TEMP_DB …"
  docker compose -f "$COMPOSE_FILE" exec -T postgres \
    psql -U lab -d postgres -c "DROP DATABASE IF EXISTS $TEMP_DB WITH (FORCE);" >/dev/null 2>&1
}
trap cleanup EXIT

echo "恢复演练：$RESTORE_FILE → 临时库 $TEMP_DB"

# 幂等：先清掉可能残留的旧临时库，再重建（避免并发/上次中断遗留）
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U lab -d postgres -c "DROP DATABASE IF EXISTS $TEMP_DB WITH (FORCE);" >/dev/null 2>&1 || true
docker compose -f "$COMPOSE_FILE" exec -T postgres createdb -U lab "$TEMP_DB" || {
  echo "失败：创建临时库 $TEMP_DB 失败（postgres 未运行？）"
  exit 1
}

# 恢复到临时库（-Fc 匹配备份格式，--exit-on-error 任一出错即失败）
if ! docker compose -f "$COMPOSE_FILE" exec -T postgres \
    pg_restore -U lab -d "$TEMP_DB" -Fc --exit-on-error < "$RESTORE_FILE" >/dev/null 2>&1; then
  echo "失败：pg_restore 恢复报错（备份文件可能损坏）"
  exit 1
fi

echo ""
echo "===== 校验 ====="

# 校验 1：表数（与生产对比 + 固定阈值兜底）
PROD_TABLES="$(psql_exec lab "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
REST_TABLES="$(psql_exec "$TEMP_DB" "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
echo "表数：临时库 $REST_TABLES / 生产 $PROD_TABLES"
[ "$REST_TABLES" = "$PROD_TABLES" ] && [ "${REST_TABLES:-0}" -ge "$MIN_TABLES" ] || {
  echo "失败：表数不一致（生产 $PROD_TABLES ≠ 恢复 $REST_TABLES，或低于阈值 $MIN_TABLES）"
  exit 1
}

# 校验 2：迁移版本号（golang-migrate 的 schema_migrations 只存当前版本一行，应 = 37）
MIG_VERSION="$(psql_exec "$TEMP_DB" "SELECT version FROM schema_migrations LIMIT 1")"
echo "已应用迁移：v${MIG_VERSION:-0} / 期望 v$EXPECTED_MIGRATIONS"
[ "${MIG_VERSION:-0}" = "$EXPECTED_MIGRATIONS" ] || {
  echo "失败：schema_migrations 版本 ${MIG_VERSION:-0} ≠ $EXPECTED_MIGRATIONS（缺失迁移）"
  exit 1
}

# 校验 3：关键行——daily_reports 最新日期非空（数据可读性抽查）
REPORT_COUNT="$(psql_exec "$TEMP_DB" "SELECT count(*) FROM daily_reports")"
LATEST_DATE="$(psql_exec "$TEMP_DB" "SELECT to_char(max(report_date),'YYYY-MM-DD') FROM daily_reports")"
echo "daily_reports：共 $REPORT_COUNT 行，最新日期 ${LATEST_DATE:-<空>}"
[ -n "$LATEST_DATE" ] && [ "$LATEST_DATE" != "" ] || {
  echo "失败：daily_reports 最新日期为空（库内无已提交日报？）"
  exit 1
}

echo ""
echo "===== 恢复演练通过 ====="
echo "备份 $RESTORE_FILE 可完整恢复（表数一致 / 迁移 001-037 完整 / 日报数据可读）"
exit 0
