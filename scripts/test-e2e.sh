#!/usr/bin/env bash
# scripts/test-e2e.sh —— 本地一键跑 Playwright E2E 冒烟（测试策略方案 §4.4 / P2）
#
# 流程：起/复用 postgres（数据库名固定 lab_e2e，每次运行重建 schema）→ 应用全量迁移
#       001-036 → 构建并启动 Go server（8000）→ 启动前端 vite dev（5173，代理 /api → 8000）
#       → 等待 /health 与前端可达 → npx playwright test → trap 清理所有进程/容器/临时库。
#
# 数据库来源（脚本自动决策，无二选一，镜像 test-go.sh 的模式）：
#   1. 已显式设置环境变量 E2E_DB_URL → 直接使用该库（脚本仍重建其 schema 并应用全量迁移；
#      仅限测试库，请勿指向业务库）；
#   2. 存在运行中的 lab-postgres 容器，且其 5432 端口确实映射到宿主（docker port 有输出）
#      → 复用（在容器里创建专用库 lab_e2e，绝不动业务库）；
#   3. 其他情况 → 起临时 postgres:16-alpine 容器（127.0.0.1:55433，trap EXIT 自动清理）。
#
# 测试数据策略：
#   - 用例用唯一前缀 e2e-<timestamp>（project code / run name / 日报原文 / 测量项）；
#   - 后端无项目/日报删除 API：脚本在结束时 DROP 整个 lab_e2e 库（专用库，天然自清理）；
#   - 批次/测试数据在用例内 teardown（软删除/标记 invalid），失败兜底由脚本清库兜住。
#
# 依赖：docker、psql、pg_isready、go、node/npm、playwright chromium 浏览器
#       （cd web-ui && npx playwright install chromium）。
# 危险：脚本会 DROP lab_e2e 库 / 重建其 schema，请勿把该库指向任何业务数据。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PG_PORT="${E2E_PG_PORT:-55433}"

for cmd in psql pg_isready docker go npm; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "错误：缺少命令 $cmd，请先安装" >&2
        exit 1
    fi
done

# ---------- 端口占用预检（不抢占用户已起的前后端） ----------
port_in_use() {
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && { exec 3>&- 3<&-; return 0; } || return 1
}
if port_in_use 8000; then
    echo "错误：端口 8000 已被占用（可能有正在运行的 lab-server），请先停掉再跑 E2E" >&2
    exit 1
fi
if port_in_use 5173; then
    echo "错误：端口 5173 已被占用（可能有正在运行的前端 dev server），请先停掉再跑 E2E" >&2
    exit 1
fi

# ---------- 状态变量与清理 ----------
TMP_CID=""
SERVER_PID=""
VITE_PID=""
BUILD_DIR=""
DB_HOST=""; DB_PORT=""; DB_USER="lab"; DB_PASSWORD="lab"; DB_NAME="lab_e2e"
DROP_DB_AT_EXIT=0

cleanup() {
    [ -n "$VITE_PID" ] && kill "$VITE_PID" >/dev/null 2>&1 || true
    [ -n "$SERVER_PID" ] && kill "$SERVER_PID" >/dev/null 2>&1 || true
    if [ "$DROP_DB_AT_EXIT" = "1" ] && [ -n "$DB_HOST" ] && [ -n "$DB_PORT" ]; then
        psql "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/postgres?sslmode=disable" \
            -v ON_ERROR_STOP=1 -q -c "DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE)" >/dev/null 2>&1 || true
    fi
    [ -n "$TMP_CID" ] && docker rm -f "$TMP_CID" >/dev/null 2>&1 || true
    [ -n "$BUILD_DIR" ] && rm -rf "$BUILD_DIR" || true
}
trap cleanup EXIT

# ---------- 1. 数据库 ----------
DB_DSN="${E2E_DB_URL:-}"

if [ -z "$DB_DSN" ]; then
    REUSE_CID="$(docker ps --filter 'name=^/lab-postgres$' --format '{{.ID}}' | head -n1 || true)"
    if [ -n "$REUSE_CID" ] && docker port "$REUSE_CID" 5432/tcp >/dev/null 2>&1; then
        echo "== 复用运行中的 lab-postgres 容器（127.0.0.1:5432，docker port 已确认映射）"
        DB_HOST="127.0.0.1"; DB_PORT=5432
        MAINT_DSN="postgres://lab:lab@127.0.0.1:5432/postgres?sslmode=disable"
        if ! psql "$MAINT_DSN" -tAc "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'" | grep -q 1; then
            psql "$MAINT_DSN" -v ON_ERROR_STOP=1 -q -c "CREATE DATABASE ${DB_NAME}"
        fi
    else
        echo "== 启动临时 postgres:16-alpine 容器（127.0.0.1:${PG_PORT}，退出自动清理）"
        TMP_CID="$(docker run --rm -d --name "hiaf-test-e2e-pg-$$" \
            -e POSTGRES_USER=lab -e POSTGRES_PASSWORD=lab -e POSTGRES_DB="${DB_NAME}" \
            -p "${PG_PORT}:5432" postgres:16-alpine)"
        DB_HOST="127.0.0.1"; DB_PORT="$PG_PORT"
    fi
    DB_DSN="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable"
    DROP_DB_AT_EXIT=1
else
    # 显式 E2E_DB_URL：解析 host/port 供 server 环境变量使用
    DB_HOST="$(printf '%s' "$DB_DSN" | sed -E 's|postgres://[^@]*@([^:/]+).*|\1|')"
    DB_PORT="$(printf '%s' "$DB_DSN" | sed -E 's|postgres://[^@]*@[^:/]+:([0-9]+).*|\1|')"
    [ -z "$DB_PORT" ] && DB_PORT=5432
    echo "== 使用显式 E2E_DB_URL：$DB_DSN（仅限测试库，将重建 schema）"
fi
export E2E_DB_URL="$DB_DSN"

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

echo "== 重建目标库 schema（专用测试库；DROP 后由迁移重建）"
# 与 test-go.sh 同一套处理：恢复 public schema 默认 ACL + 清理 ask_reader 角色（迁移 033 会重建）
psql "$DB_DSN" -v ON_ERROR_STOP=1 -q \
    -c "DROP SCHEMA public CASCADE;" \
    -c "DROP EXTENSION IF EXISTS pgcrypto CASCADE;" \
    -c "CREATE SCHEMA public;" \
    -c "GRANT USAGE, CREATE ON SCHEMA public TO PUBLIC;" \
    -c "DROP ROLE IF EXISTS ask_reader;"

echo "== 应用全量迁移（migrations/*.up.sql，001-036）"
for f in "$REPO_ROOT"/migrations/*.up.sql; do
    echo "   applying $(basename "$f")"
    psql "$DB_DSN" -v ON_ERROR_STOP=1 -q -f "$f"
done

# ---------- 2. Go server ----------
echo "== 构建 Go server（go build ./go-server）"
BUILD_DIR="$(mktemp -d /tmp/hiaf-e2e-XXXXXX)"
( cd "$REPO_ROOT/go-server" && go build -o "$BUILD_DIR/lab-server" . )

echo "== 启动 Go server（127.0.0.1:8000）"
PORT=8000 \
DB_HOST="$DB_HOST" DB_PORT="$DB_PORT" DB_USER="$DB_USER" DB_PASSWORD="$DB_PASSWORD" DB_NAME="$DB_NAME" \
JWT_SECRET="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')" \
SERVICE_TOKEN="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')" \
INFLUXDB_ADDR="http://127.0.0.1:18086" INFLUXDB_ORG="e2e" INFLUXDB_BUCKET="e2e" INFLUXDB_TOKEN="e2e-token" \
EPICS_GATEWAY_ADDR="http://127.0.0.1:18087" \
PY_AGENT_INTERPRET_URL="http://127.0.0.1:18099" PY_AGENT_INTERNAL_TOKEN="e2e-agent-token" \
SOURCE_GATE_ENABLED=false \
TODOS_SCHEDULER_ENABLED=false \
SELF_BASE_URL="http://127.0.0.1:8000" \
"$BUILD_DIR/lab-server" >/tmp/hiaf-e2e-server.log 2>&1 &
SERVER_PID=$!

echo "== 等待 /health（最长 60s）"
tries=60
until curl -fsS --max-time 2 http://127.0.0.1:8000/health >/dev/null 2>&1; do
    tries=$((tries - 1))
    if [ "$tries" -le 0 ]; then
        echo "错误：server /health 超时，日志见 /tmp/hiaf-e2e-server.log" >&2
        tail -30 /tmp/hiaf-e2e-server.log >&2 || true
        exit 1
    fi
    sleep 1
done

# ---------- 3. 前端 dev server ----------
echo "== 启动前端 vite dev（127.0.0.1:5173）"
# 直接 exec node 启动 vite（不经 npm wrapper，避免 trap kill 只杀 npm、vite 子进程残留占端口）
( cd "$REPO_ROOT/web-ui" && exec node node_modules/vite/bin/vite.js --host 127.0.0.1 ) >/tmp/hiaf-e2e-vite.log 2>&1 &
VITE_PID=$!

echo "== 等待前端可达（最长 90s）"
tries=90
until curl -fsS --max-time 2 http://127.0.0.1:5173/login -o /dev/null 2>/dev/null; do
    tries=$((tries - 1))
    if [ "$tries" -le 0 ]; then
        echo "错误：前端 5173 超时，日志见 /tmp/hiaf-e2e-vite.log" >&2
        tail -30 /tmp/hiaf-e2e-vite.log >&2 || true
        exit 1
    fi
    sleep 1
done

# ---------- 4. Playwright ----------
echo "== 运行 Playwright E2E（8 条冒烟用例）"
set +e
( cd "$REPO_ROOT/web-ui" && npx playwright test )
RC=$?
set -e

if [ "$RC" -ne 0 ]; then
    echo "== E2E 失败（退出码 $RC）。server 日志：/tmp/hiaf-e2e-server.log，vite 日志：/tmp/hiaf-e2e-vite.log" >&2
fi
exit "$RC"
