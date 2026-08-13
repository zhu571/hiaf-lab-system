#!/usr/bin/env bash
# scripts/test-go.sh —— 本地一键跑 Go 全量测试（含依赖真实 Postgres 的 db 集成测试）
#
# 用途：CI 的 go-test job（.github/workflows/ci.yml）设置 TEST_DATABASE_URL 并应用全量迁移，
# 让 14 个 *_db_test.go 真正跑起来；本脚本在本地复刻同一套环境，消除「本地绿、CI 红」差异。
#
# 流程：确定数据库 → 等待 pg_isready → 重建目标库 schema → 按序应用 migrations/*.up.sql
#       （001-038 全量）→ cd go-server && TEST_DATABASE_URL=... go test -race -count=1 ./...
#
# 数据库来源（脚本自动决策，无二选一）：
#   1. 已显式设置环境变量 TEST_DATABASE_URL → 直接使用该库（脚本仍会重建其 schema 并应用全量迁移，仅限测试库）；
#   2. 存在运行中的 lab-postgres 容器，且其 5432 端口确实映射到宿主（docker port 有输出）
#      → 复用（确保 lab_test 库存在）；
#   3. 其他情况 → 起临时 postgres:16-alpine 容器（127.0.0.1:55432，trap EXIT 自动清理）。
#
# 默认 DSN（未显式设置 TEST_DATABASE_URL 时导出）：
#   postgres://lab:lab@127.0.0.1:55432/lab_test?sslmode=disable
#   （复用 lab-postgres 时端口为 5432。注意：只凭容器名不能断定容器连得通——
#   若容器未映射宿主端口，127.0.0.1:5432 可能就是本机系统 postgres，脚本绝不误连。）
#
# 依赖：docker、psql、pg_isready（postgresql-client）。
# 危险：脚本会重建目标库（DROP SCHEMA public CASCADE 等），请勿指向非测试库。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v psql >/dev/null 2>&1 || ! command -v pg_isready >/dev/null 2>&1; then
    echo "错误：需要 postgresql-client（psql / pg_isready），请先安装" >&2
    exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
    echo "错误：需要 docker（用于起/复用 postgres 容器）" >&2
    exit 1
fi

TMP_CID=""

cleanup() {
    if [ -n "$TMP_CID" ]; then
        docker rm -f "$TMP_CID" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

DB_DSN="${TEST_DATABASE_URL:-}"

if [ -z "$DB_DSN" ]; then
    REUSE_CID="$(docker ps --filter 'name=^/lab-postgres$' --format '{{.ID}}' | head -n1 || true)"
    if [ -n "$REUSE_CID" ] && docker port "$REUSE_CID" 5432/tcp >/dev/null 2>&1; then
        echo "== 复用运行中的 lab-postgres 容器（127.0.0.1:5432，docker port 已确认映射）"
        HOST_PORT=5432
        MAINT_DSN="postgres://lab:lab@127.0.0.1:${HOST_PORT}/postgres?sslmode=disable"
        if ! psql "$MAINT_DSN" -tAc "SELECT 1 FROM pg_database WHERE datname='lab_test'" | grep -q 1; then
            psql "$MAINT_DSN" -v ON_ERROR_STOP=1 -q -c "CREATE DATABASE lab_test"
        fi
    else
        if [ -n "$REUSE_CID" ]; then
            echo "== lab-postgres 存在但未把 5432 映射到宿主，不冒险连 127.0.0.1:5432（可能是系统 postgres）"
        fi
        echo "== 启动临时 postgres:16-alpine 容器（127.0.0.1:55432，退出自动清理）"
        TMP_CID="$(docker run --rm -d --name "hiaf-test-pg-$$" \
            -e POSTGRES_USER=lab -e POSTGRES_PASSWORD=lab -e POSTGRES_DB=lab_test \
            -p 55432:5432 postgres:16-alpine)"
        HOST_PORT=55432
    fi
    DB_DSN="postgres://lab:lab@127.0.0.1:${HOST_PORT}/lab_test?sslmode=disable"
fi
export TEST_DATABASE_URL="$DB_DSN"

echo "== 等待数据库就绪（pg_isready，最长 30s）"
tries=30
until pg_isready -d "$DB_DSN" -q; do
    tries=$((tries - 1))
    if [ "$tries" -le 0 ]; then
        echo "错误：pg_isready 超时：$DB_DSN" >&2
        exit 1
    fi
    sleep 1
done

echo "== 重建目标库（仅测试库专用；DROP 后由迁移重建）"
# 注意：DROP SCHEMA public 后必须恢复 public schema 的默认 ACL（PUBLIC 的 USAGE+CREATE），
# 否则 ask_reader 等角色有表级 SELECT 授权也解析不到表（42P01 relation does not exist）。
psql "$DB_DSN" -v ON_ERROR_STOP=1 -q \
    -c "DROP SCHEMA public CASCADE;" \
    -c "DROP EXTENSION IF EXISTS pgcrypto CASCADE;" \
    -c "CREATE SCHEMA public;" \
    -c "GRANT USAGE, CREATE ON SCHEMA public TO PUBLIC;" \
    -c "DROP ROLE IF EXISTS ask_reader;"

echo "== 应用全量迁移（migrations/*.up.sql，按序号 001-038）"
for f in "$REPO_ROOT"/migrations/*.up.sql; do
    echo "   applying $(basename "$f")"
    psql "$DB_DSN" -v ON_ERROR_STOP=1 -q -f "$f"
done

echo "== go test -race -count=1 -p 1 ./...（TEST_DATABASE_URL=$TEST_DATABASE_URL）"
# -p 1：db 测试共用同一 TEST_DATABASE_URL，跨包固定 UUID 种子并行会撞主键 flaky
#（如 agent 与 issues 的 b140 用户），按测试策略方案 §4.3.1 串行化包级测试。
# -coverprofile：覆盖率透明化（P3，方案 §5）——只报告不设硬阈值，人工盯下降趋势。
COVERAGE_OUT="${COVERAGE_OUT:-/tmp/hiaf-coverage.out}"
cd "$REPO_ROOT/go-server"
go test -race -count=1 -p 1 -coverprofile="$COVERAGE_OUT" ./...

echo
echo "================ Go 覆盖率汇总（不设门禁，人工盯） ================"
go tool cover -func="$COVERAGE_OUT" | tail -1
echo
echo "核心包明细（方案 §5 建议关注 ≥60%，参考值非硬门禁）："
# 从 coverprofile（行格式「file:range 语句数 覆盖数」，-race 下 mode: atomic）按包聚合语句级覆盖率
# 注意：变量赋值必须用 -v（mawk 不认程序后的 name=value 参数，会当文件打开）
awk -v names="auth projects logs issues testdata runs ask alert" '
BEGIN { n = split(names, arr, " ") }
/^mode:/ { next }
$2 + 0 > 0 {
    for (i = 1; i <= n; i++) {
        name = arr[i]
        # 路径形态不固定：脚本 cd 到 go-server 后 go test 输出 cwd 相对路径 auth/x.go，
        # 仓库根相对/绝对/模块路径前缀时则形如 [..]/go-server/auth/x.go —— 统一按
        # 「包名作为路径段」(^|/)name/ 匹配；8 个包名在 go-server 目录树无同名子串目录，无误伤。
        if ($1 ~ ("(^|/)" name "/")) {
            total[name] += $2
            covered[name] += ($3 > 0 ? $2 : 0)
            seen[name] = 1
        }
    }
}
END {
    for (i = 1; i <= n; i++) {
        name = arr[i]
        if (!seen[name]) { printf "  %-10s 无覆盖数据（包内无测试）\n", name; continue }
        pct = (total[name] > 0) ? (covered[name] * 100 / total[name]) : 0
        printf "  %-10s %6.1f%%  (%d/%d 条语句)\n", name, pct, covered[name], total[name]
    }
}' "$COVERAGE_OUT"
