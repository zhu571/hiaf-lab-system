#!/usr/bin/env bash
# scripts/test-all.sh —— 本地一键全量测试（P3，测试策略方案 §4.6）
#
# 串联 5 步，任一步失败即退出非零并标明失败步骤（set -euo pipefail + 逐步 echo）：
#   [1/5] 迁移 up/down 验证   —— scripts/test-migrations.sh（临时 postgres 容器）
#   [2/5] Go 全量测试         —— scripts/test-go.sh（复用/起 postgres + 全量迁移 + race + 覆盖率）
#   [3/5] py-agent unittest   —— scripts/test-py.sh（unittest + coverage，91 用例）
#   [4/5] 前端 vitest         —— web-ui: npm test（4 文件 47 用例，CI frontend-test 同一命令）
#   [5/5] 前端构建 + static 一致性 —— npm run build 后 diff -rq web-ui/dist go-server/static
#                                          （防旧 embed 白屏，与 CI frontend-test 的 Check static sync 同一校验）
#
# 前置依赖：docker、postgresql-client（psql/pg_isready）、node（web-ui 依赖已 npm ci 安装）。
# 每步输出经 tee 保留到 /tmp/hiaf-test-all-<pid>/；成功退出时自动清理，失败/中断时
# 日志目录保留（失败消息会提示路径，排查后手动删除）。
# 汇总：结束后打印各层测试数量/通过状态。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ ! -d "$REPO_ROOT/web-ui/node_modules" ]; then
    echo "提示：web-ui/node_modules 不存在，请先执行 cd web-ui && npm ci（CI 同款安装）" >&2
    exit 1
fi

LOG_DIR="/tmp/hiaf-test-all-$$"
mkdir -p "$LOG_DIR"

# 成功（退出码 0）清理日志目录；失败/中断保留，便于按失败消息提示的路径排查。
cleanup() {
    local rc=$?
    if [ "$rc" -eq 0 ]; then
        rm -rf "$LOG_DIR"
    else
        echo "完整日志保留在：$LOG_DIR（排查后手动删除：rm -rf \"$LOG_DIR\"）" >&2
    fi
}
trap cleanup EXIT

STEP_TOTAL=5
step_no=0

run_step() {
    local name="$1" log="$2"
    shift 2
    step_no=$((step_no + 1))
    echo
    echo "========================================================================"
    echo "[$step_no/$STEP_TOTAL] $name"
    echo "========================================================================"
    if ! "$@" 2>&1 | tee "$log"; then
        echo
        echo "[失败] 步骤 $step_no/$STEP_TOTAL：$name —— 完整日志：$log" >&2
        exit 1
    fi
    echo "[通过] 步骤 $step_no/$STEP_TOTAL：$name"
}

# [1/5] 迁移验证
run_step "迁移 up/down 验证（全量 up → 反序 down）" "$LOG_DIR/migrations.log" \
    bash "$REPO_ROOT/scripts/test-migrations.sh"

# [2/5] Go 全量测试（含 db 集成 + 覆盖率汇总）
run_step "Go 全量测试（race + db 集成 + 覆盖率汇总）" "$LOG_DIR/go.log" \
    bash "$REPO_ROOT/scripts/test-go.sh"

# [3/5] py-agent unittest + 覆盖率
run_step "py-agent unittest + 覆盖率" "$LOG_DIR/py.log" \
    bash "$REPO_ROOT/scripts/test-py.sh"

# [4/5] 前端 vitest
run_step "前端 vitest 单元测试" "$LOG_DIR/vitest.log" \
    npm --prefix "$REPO_ROOT/web-ui" test

# [5/5] 前端构建 + static 一致性
run_step "前端构建（vue-tsc + vite build）" "$LOG_DIR/build.log" \
    npm --prefix "$REPO_ROOT/web-ui" run build
echo "== 检查 web-ui/dist 与 go-server/static 一致性（防旧 embed 白屏）"
if ! diff -rq "$REPO_ROOT/web-ui/dist" "$REPO_ROOT/go-server/static"; then
    echo
    echo "[失败] 步骤 5/5：web-ui/dist 与 go-server/static 不一致" >&2
    echo "       请执行：rsync -a --delete web-ui/dist/ go-server/static/ 后重新提交" >&2
    exit 1
fi

# ---- 汇总 ----
# 各工具日志可能带 ANSI 颜色码（vitest 的 \x1b[36m 等色码在行首，会破坏 ^ 行首锚定，
# 如 '^ *Tests' 匹配不到），解析前统一去色预处理；-a 兜底防带色内容被当作二进制。
STRIP_ANSI='s/\x1b\[[0-9;?]*[a-zA-Z]//g'
MIG_UP="$(sed -E "$STRIP_ANSI" "$LOG_DIR/migrations.log" | grep -ac '^\[OK\].* up (' || true)"
MIG_DOWN="$(sed -E "$STRIP_ANSI" "$LOG_DIR/migrations.log" | grep -ac '^\[OK\].* down$' || true)"
GO_PKGS="$(sed -E "$STRIP_ANSI" "$LOG_DIR/go.log" | grep -ac '^ok  ' || true)"
PY_RAN="$(sed -E "$STRIP_ANSI" "$LOG_DIR/py.log" | grep -am1 '^Ran ' || true)"
VITEST_TESTS="$(sed -E "$STRIP_ANSI" "$LOG_DIR/vitest.log" | grep -am1 -E '^ *Tests +' || true)"

echo
echo "============================ 全量测试汇总 ============================"
echo "  [1/5] 迁移 up/down 验证     : ${MIG_UP:-0} 个 up + ${MIG_DOWN:-0} 个 down 全部通过"
echo "  [2/5] Go 全量测试           : ${GO_PKGS:-0} 个包通过（race + db 集成 + 覆盖率见上方）"
echo "  [3/5] py-agent unittest     : ${PY_RAN:-未解析到（查看日志）}"
echo "  [4/5] 前端 vitest           : ${VITEST_TESTS:-未解析到（查看日志）}"
echo "  [5/5] 前端构建 + static 一致性: 构建成功，web-ui/dist 与 go-server/static 一致"
echo "========================================================================"
echo "全部通过：5/5 步成功"
