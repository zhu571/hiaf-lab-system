#!/bin/bash
set -euo pipefail

# ============================================================
# hiaf-lab-system 一键更新脚本（宿主机手工执行的兜底脚本）
# 用法: ./update.sh [--force] [--dry-run] [--no-rollback]
#   --force        跳过变更检测，全量重建
#   --dry-run      仅检测变更，不执行实际操作
#   --no-rollback  失败时不回滚（调试用）
#
# 注意：本脚本设计为在宿主机上手工执行。从 server 容器内执行时，
# 仓库是只读挂载（git pull / 构建上下文无法写入），且重建 server
# 容器会杀死脚本自身进程，无法完成更新。Web 触发更新请使用
# UPDATE_ENGINE=go（独立 runner 容器），本脚本仅作宿主机兜底。
# ============================================================

# 并发锁：同一时刻只允许一个更新进程
exec 9>"${UPDATE_LOCK_FILE:-/tmp/lab-update.lock}"
flock -n 9 || { echo "another update in progress"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="$REPO_ROOT/deploy/docker-compose.yml"
cd "$REPO_ROOT"

FORCE=false
DRY_RUN=false
NO_ROLLBACK=false
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=true ;;
    --dry-run) DRY_RUN=true ;;
    --no-rollback) NO_ROLLBACK=true ;;
    *) echo "Unknown arg: $arg"; exit 1 ;;
  esac
done

# ---- 色彩输出 ----
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${GREEN}[UPDATE]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()  { echo -e "${RED}[ERROR]${NC} $*"; }

# ---- 会话日志（由 Go system 模块经 setsid 启动时启用）----
# 脚本自身以"行缓冲"把每行输出 tee 到 UPDATE_LOG_FILE（Go 侧 tail 该文件推送 SSE）。
# 脚本脱离 Go 进程/容器运行后，日志与结果 marker 仍由脚本自己落盘，不依赖父进程存活。
if [ -n "${UPDATE_SESSION_ID:-}" ]; then
  : "${UPDATE_LOG_FILE:=/tmp/lab-update-${UPDATE_SESSION_ID}.log}"
  : "${UPDATE_DONE_FILE:=/tmp/lab-update-${UPDATE_SESSION_ID}.done}"
  exec > >(stdbuf -oL tee -a "$UPDATE_LOG_FILE")
  exec 2>&1

  # EXIT trap 写 done marker（成功/失败/回滚/被 kill 前都会执行，exit_code=$?）
  _write_marker() {
    local code=$?
    local sha_old="${OLD_SHA:-}"
    local sha_new="${NEW_SHA:-}"
    printf '{"exit_code":%d,"old_sha":"%s","new_sha":"%s","ended_at":"%s"}' \
      "$code" "$sha_old" "$sha_new" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$UPDATE_DONE_FILE"
    exit "$code"
  }
  trap _write_marker EXIT
fi

# ---- git 网络保护：禁止交互式凭据提示，低网速超时兜底 ----
export GIT_TERMINAL_PROMPT=0
export GIT_HTTP_LOW_SPEED_LIMIT=1024   # <1KB/s 视为低网速
export GIT_HTTP_LOW_SPEED_TIME=30      # 持续 30s 则 git 主动失败（不挂死）
GIT_TIMEOUT=120                        # 单条 git 命令上限（秒）

# ---- 更新分支与通知地址（可被环境变量覆盖）----
BRANCH="${UPDATE_BRANCH:-$(git rev-parse --abbrev-ref HEAD)}"
NTFY_URL="${UPDATE_NTFY_URL:-http://localhost:8085/lab-system}"

# ---- 回滚函数 ----
rollback() {
  # 回滚路径上任何一步失败都不应中断后续步骤（尤其是通知），
  # 函数体内关闭 set -e，每步显式兜底。
  set +e
  log ""
  warn "========== 开始回滚 =========="

  if [ -z "${OLD_SHA:-}" ]; then
    err "无旧 commit 记录，无法回滚。"
    set -e
    exit 1
  fi

  log "恢复代码到 $OLD_SHORT …"
  git checkout "$OLD_SHA" || err "git checkout $OLD_SHA 失败，继续尽力回滚"

  if echo "${AFFECTED_SERVICES:-}" | grep -q "migrate"; then
    err "========== 迁移变更，回滚阻塞！ =========="
    err "数据库 schema 可能已被新迁移修改，自动回滚存在风险。"
    err "请先手动回滚迁移，然后重新执行回滚或部署："
    err "  1. 运行 migrate down 回退 schema"
    err "  2. 验证服务状态后重新部署"
    if [ -n "${BACKUP_FILE:-}" ] && [ -f "$BACKUP_FILE" ]; then
      err "数据库备份: $BACKUP_FILE"
    fi
    err "=============================================="
    curl -s -X POST "$NTFY_URL" \
      -H "Title: 系统更新失败-迁移变更阻塞" \
      -H "Priority: urgent" \
      -H "Tags: warning,skull" \
      -d "Rollback to $OLD_SHORT blocked: schema may have changed. Manual migrate down required." >/dev/null 2>&1 || true
    # 回滚被阻塞（未执行），工作区切回分支上的新代码
    git checkout "$BRANCH" 2>/dev/null || true
    set -e
    exit 1
  fi

  if [ -n "${BACKUP_FILE:-}" ] && [ -f "$BACKUP_FILE" ]; then
    warn "数据库备份保留: $BACKUP_FILE"
  fi

  local rollback_list
  if echo "${AFFECTED_SERVICES:-}" | grep -q "ALL"; then
    rollback_list="server py-agent py-agent-interpret epics-gateway ioc migrate"
  else
    rollback_list="${AFFECTED_SERVICES:-}"
  fi
  log "用旧代码重建受影响服务 …"
  for svc in $rollback_list; do
    docker compose -f "$COMPOSE_FILE" build "$svc"
    docker compose -f "$COMPOSE_FILE" up -d --no-deps "$svc"
  done

  curl -s -X POST "$NTFY_URL" \
    -H "Title: 系统更新失败-已回滚" \
    -H "Priority: urgent" \
    -H "Tags: warning" \
    -d "Rollback to $OLD_SHORT after update ${NEW_SHORT:-?} failed" >/dev/null 2>&1 || true

  # 回滚后工作区必须停留在旧代码：切回分支并硬重置到 OLD_SHA。
  # 若只 checkout 分支，仓库停在新代码而容器跑旧镜像，后续更新会空转。
  git checkout "$BRANCH" 2>/dev/null || true
  git reset --hard "$OLD_SHA" 2>/dev/null || true

  warn "========== 回滚完成 =========="
  warn "请检查服务状态并排查失败原因。"
  set -e
  exit 1
}

# ---- 步骤 1：预检 ----
log "===== 步骤 1/7：预检 ====="

if ! command -v docker &>/dev/null; then
  err "Docker 未安装或不在 PATH"; exit 1
fi
if ! docker compose version &>/dev/null; then
  err "docker compose (v2) 不可用"; exit 1
fi

# busybox df 不支持 --output，用 POSIX 兼容写法取可用空间（KB）
AVAIL=$(df -k "$REPO_ROOT" | awk 'NR==2{print $4}')
if [ "${AVAIL:-0}" -lt 2097152 ]; then
  warn "磁盘可用空间 < 2 GB，请清理后重试"
fi

for s in db_password jwt_key influxdb_token agent_password; do
  if [ ! -f "$REPO_ROOT/deploy/secrets/${s}.txt" ]; then
    err "缺少 secret 文件: deploy/secrets/${s}.txt"; exit 1
  fi
done

# ---- 步骤 2：记录当前状态 ----
log "===== 步骤 2/7：记录当前状态 ====="

OLD_SHA=$(git rev-parse HEAD)
OLD_SHORT="${OLD_SHA:0:7}"
log "当前 commit: $OLD_SHORT"

BACKUP_FILE="${UPDATE_BACKUP_DIR:-/tmp}/lab-db-backup-${OLD_SHORT}-$(date +%Y%m%d_%H%M%S).sql"
if [ "$DRY_RUN" = false ]; then
  mkdir -p "${UPDATE_BACKUP_DIR:-/tmp}"
  log "备份数据库 → $BACKUP_FILE"
  docker compose -f "$COMPOSE_FILE" exec -T postgres \
    pg_dump -U lab lab > "$BACKUP_FILE" 2>/dev/null || {
    warn "数据库备份失败（postgres 未运行？），继续…"
    BACKUP_FILE=""
  }
fi

# ---- 步骤 3：拉取代码 ----
log "===== 步骤 3/7：git pull ====="

timeout "$GIT_TIMEOUT" git fetch origin
BEFORE_PULL=$(git rev-parse HEAD)

if timeout "$GIT_TIMEOUT" git pull --ff-only origin "$BRANCH" 2>&1; then
  NEW_SHA=$(git rev-parse HEAD)
  NEW_SHORT="${NEW_SHA:0:7}"
  log "已更新: ${OLD_SHORT} → ${NEW_SHORT}"
elif $FORCE; then
  warn "git pull 失败，但 --force 模式继续（使用当前 HEAD）"
  NEW_SHA=$BEFORE_PULL
  NEW_SHORT="${NEW_SHA:0:7}"
else
  err "git pull 失败，中止。请手动解决冲突。"
  exit 1
fi

if [ "$BEFORE_PULL" = "$NEW_SHA" ] && [ "$FORCE" = false ]; then
  log "代码无变更，跳过更新。"
  exit 0
fi

# ---- 步骤 4：变更检测 ----
log "===== 步骤 4/7：变更检测 ====="

CHANGED_FILES=$(git diff --name-only "$OLD_SHA" "$NEW_SHA" 2>/dev/null || echo "")

if [ -z "$CHANGED_FILES" ] && [ "$FORCE" = false ]; then
  log "无文件变更，跳过构建。"
  exit 0
fi

detect_services() {
  local svc=""
  local has_all=false
  for f in $CHANGED_FILES; do
    case "$f" in
      deploy/docker-compose.yml|deploy/Dockerfile|deploy/.env*)
        echo "ALL"
        return
        ;;
      deploy/Dockerfile.migrate)
        svc="$svc migrate" ;;
      web-ui/*)
        svc="$svc server" ;;
      go-server/epics-gateway/*)
        svc="$svc epics-gateway" ;;
      go-server/*)
        svc="$svc server" ;;
      py-agent/ioc/*|py-agent/ioc/Dockerfile)
        svc="$svc ioc" ;;
      py-agent/*|py-agent/Dockerfile)
        svc="$svc py-agent py-agent-interpret" ;;
      migrations/*)
        svc="$svc migrate" ;;
      deploy/*)
        svc="$svc server" ;;
    esac
  done
  svc="${svc# }"
  if [ -z "$svc" ]; then
    echo "$CHANGED_FILES" | grep -q 'migrations/' && echo "migrate" && return
    echo "none"
  else
    echo "$svc" | tr ' ' '\n' | sort -u | tr '\n' ' '
  fi
}

AFFECTED_SERVICES=$(detect_services | xargs)
if $FORCE; then
  log "--force: 强制全量重建"
  AFFECTED_SERVICES="ALL"
fi
AFFECTED_LIST=$(echo "$AFFECTED_SERVICES" | tr ' ' '\n' | sed '/^$/d')

log "变更文件数: $(echo "$CHANGED_FILES" | wc -l)"
log "受影响服务: ${AFFECTED_SERVICES:-none}"

if [ "$DRY_RUN" = true ]; then
  echo ""
  echo "===== DRY RUN 变更详情 ====="
  echo "$CHANGED_FILES" | while read -r f; do echo "  $f"; done
  echo ""
  echo "受影响服务: ${AFFECTED_SERVICES:-无}"
  echo "===== DRY RUN 结束 ====="
  exit 0
fi

if [ "$AFFECTED_SERVICES" = "none" ]; then
  log "无服务需要更新。"
  exit 0
fi

# ---- 步骤 5：按依赖顺序构建 ----
log "===== 步骤 5/7：构建镜像 ====="

if echo "$AFFECTED_SERVICES" | grep -q "ALL"; then
  warn "compose 文件或 Dockerfile 变更，执行全量重建"
  docker compose -f "$COMPOSE_FILE" build --pull server py-agent py-agent-interpret epics-gateway ioc migrate
  AFFECTED_LIST=$(echo "server py-agent py-agent-interpret epics-gateway ioc migrate" | tr ' ' '\n')
else
  for svc in epics-gateway ioc py-agent-interpret migrate; do
    if echo "$AFFECTED_LIST" | grep -q "$svc"; then
      log "构建 $svc …"
      docker compose -f "$COMPOSE_FILE" build "$svc"
      if [ "$svc" = "py-agent-interpret" ]; then
        AFFECTED_SERVICES=$(echo "$AFFECTED_SERVICES" | sed 's/py-agent //')
      fi
    fi
  done

  if echo "$AFFECTED_LIST" | grep -q "server"; then
    log "构建 server（含前端）…"
    docker compose -f "$COMPOSE_FILE" build server
  fi

  if echo "$AFFECTED_LIST" | grep -q "py-agent"; then
    log "py-agent 与 py-agent-interpret 共用镜像，已构建。"
  fi
fi

# ---- 步骤 6：滚动重启 + 迁移 ----
log "===== 步骤 6/7：滚动更新 ====="

restart_service() {
  local svc=$1
  local max_wait=${2:-30}
  log "重启 $svc …"
  docker compose -f "$COMPOSE_FILE" up -d --no-deps "$svc"
  sleep 2
  for i in $(seq 1 "$max_wait"); do
    # 纯 docker CLI：compose v2 ps --format json 是 NDJSON，且环境未必有 python3
    STATUS="missing"
    CID=$(docker compose -f "$COMPOSE_FILE" ps -q "$svc" 2>/dev/null | head -1)
    if [ -n "$CID" ]; then
      STATUS=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$CID" 2>/dev/null || echo "missing")
    fi
    case "$STATUS" in
      healthy) log "$svc 健康 ($((i*2))s)"; return 0 ;;
      running) log "$svc 运行中"; return 0 ;;
    esac
    sleep 2
  done
  err "$svc 健康检查超时"
  return 1
}

# migrate 必须先于所有业务容器重启：避免新代码对旧 schema 运行
if ! docker compose -f "$COMPOSE_FILE" ps postgres | grep -q "(healthy)"; then
  warn "postgres 不健康，尝试重启…"
  docker compose -f "$COMPOSE_FILE" up -d postgres
  sleep 5
fi

if echo "$AFFECTED_LIST" | grep -q "migrate"; then
  log "运行数据库迁移 …"
  if docker compose -f "$COMPOSE_FILE" run --rm migrate; then
    log "迁移成功"
  else
    err "迁移失败！"
    $NO_ROLLBACK || rollback
    exit 1
  fi
fi

for svc in epics-gateway ioc py-agent-interpret; do
  if echo "$AFFECTED_LIST" | grep -q "$svc"; then
    restart_service "$svc" 30 || {
      err "$svc 重启失败"
      $NO_ROLLBACK || rollback
      exit 1
    }
  fi
done

if echo "$AFFECTED_LIST" | grep -q "server"; then
  restart_service "server" 15 || {
    err "server 启动失败，查看日志:"
    docker compose -f "$COMPOSE_FILE" logs --tail 50 server
    $NO_ROLLBACK || rollback
    exit 1
  }
fi

if echo "$AFFECTED_LIST" | grep -q "py-agent"; then
  restart_service "py-agent" 30 || {
    err "py-agent 启动失败"
    $NO_ROLLBACK || rollback
    exit 1
  }
fi

# ---- 步骤 7：最终健康检查 ----
log "===== 步骤 7/7：全栈健康检查 ====="

ALL_HEALTHY=true

check_health() {
  local svc=$1
  local state="missing"
  local cid
  cid=$(docker compose -f "$COMPOSE_FILE" ps -q "$svc" 2>/dev/null | head -1)
  if [ -n "$cid" ]; then
    state=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$cid" 2>/dev/null || echo "missing")
  fi

  case "$state" in
    healthy|running)
      echo -e "  ${GREEN}OK${NC}  $svc: $state" ;;
    *)
      echo -e "  ${RED}BAD${NC} $svc: $state"
      ALL_HEALTHY=false ;;
  esac
}

for svc in postgres influxdb grafana ntfy epics-gateway ioc py-agent-interpret server py-agent; do
  check_health "$svc"
done

log ""
if $ALL_HEALTHY; then
  log "=========================================="
  log "  更新成功！${OLD_SHORT} → ${NEW_SHORT}"
  log "=========================================="
  curl -s -X POST "$NTFY_URL" \
    -H "Title: 系统更新成功" \
    -H "Priority: default" \
    -H "Tags: white_check_mark" \
    -d "Updated ${OLD_SHORT} → ${NEW_SHORT}" >/dev/null 2>&1 || true
else
  err "部分服务不健康，请检查！"
  $NO_ROLLBACK || rollback
  exit 1
fi

if [ -n "${BACKUP_FILE:-}" ] && [ -f "$BACKUP_FILE" ]; then
  log "数据库备份保留: $BACKUP_FILE"
fi

log "完成。"
