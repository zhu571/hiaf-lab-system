# 一键更新方案 — hiaf-lab-system

> 目标：在 gascell 服务器上执行一个命令，完成 git pull → 变更检测 → 按依赖顺序重建 → 迁移 → 健康检查，失败自动回滚。

---

## 1. 架构概览

```
┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│  pull    │───▶│  detect  │───▶│  build   │───▶│  deploy  │
│  code    │    │ changes  │    │ images   │    │+migrate  │
└──────────┘    └──────────┘    └──────────┘    └──────────┘
                     │                               │
                     ▼                               ▼
               skip if none                  health check × N
                                             fail → rollback
```

### 服务依赖 DAG（部署顺序）

```
     [epics-gateway]──┐
     [py-agent-interpret]──┐
     [ioc]                 │
                           ▼
     [postgres:healthy] ──▶ [migrate:success] ──▶ [server:healthy] ──▶ [py-agent:healthy]
```

- **无需重建的服务**：postgres、influxdb、grafana、ntfy（使用预构建镜像，仅配置变更时重启）。
- **需要构建的服务**：server、py-agent、py-agent-interpret、epics-gateway、ioc、migrate。

---

## 2. 变更检测策略

使用 `git diff --name-only` 对比更新前后的 commit SHA，按目录映射到受影响的服务。

### 映射表

| 变更路径 | 受影响服务 | 说明 |
|----------|-----------|------|
| `web-ui/**` | `server` | 前端嵌入 Go 二进制，需重建 server |
| `go-server/**`（不含 `epics-gateway/`） | `server` | Go 后端代码变更 |
| `go-server/epics-gateway/**` | `epics-gateway` | EPICS 网关独立容器 |
| `py-agent/**`（不含 `ioc/`） | `py-agent`, `py-agent-interpret` | 两个服务共用同一镜像 |
| `py-agent/ioc/**` | `ioc` | 虚拟 IOC 独立容器 |
| `migrations/**` | `migrate`（一次性任务） | 总是运行迁移 |
| `deploy/**` | `server` + 全部 | compose 文件变更 → 全量重建 |
| `AGENTS.md`, `docs/**`, `*.md` | 无 | 跳过 |

### 特殊规则

1. **`deploy/docker-compose.yml` 变更** → 强制全量重建（`docker compose up -d --build`）。
2. **Dockerfile 自身变更** → 对应服务必须 `--no-cache` 重建。
3. **`requirements.txt`、`go.mod`、`go.sum`、`package*.json` 变更** → 对应服务需构建（Docker layer cache 自动处理依赖层失效）。
4. **仅 migration 文件变更，无代码变更** → 只跑 migrate，不重建任何容器。

---

## 3. Docker Layer Cache 优化

### 已有优化（无需改动）

各 Dockerfile 已按最佳实践分层：

```dockerfile
# deploy/Dockerfile — Go + 前端
COPY web-ui/package.json web-ui/package-lock.json ./   # ← 依赖层先用
RUN npm ci                                              # ← 缓存命中率高
COPY web-ui/ ./                                         # ← 源码层后用
RUN npm run build

COPY go-server/go.mod go-server/go.sum ./               # ← Go 依赖层先用
RUN go mod download                                     # ← 缓存命中率高
COPY go-server/ ./                                      # ← 源码层后用
RUN go build
```

```dockerfile
# py-agent/Dockerfile
COPY requirements.txt .                                 # ← 依赖层
RUN pip install --no-cache-dir -r requirements.txt       # ← 缓存命中率高
COPY . .                                                # ← 源码层
```

### 构建命令建议

```bash
# 正常构建（使用缓存）
docker compose -f deploy/docker-compose.yml build <service>

# 依赖文件变更时强制重建依赖层（自动触发，无需 --no-cache）
# 因为 COPY 的 requirements.txt/package.json go.mod 变更 → layer hash 变更 → 缓存 MISS

# 仅 Dockerfile 自身变更时需要 --no-cache
docker compose -f deploy/docker-compose.yml build --no-cache <service>
```

### 缓存共享（可选优化）

如果在内网部署 BuildKit 或 registry：

```bash
# 启用 BuildKit 内联缓存
docker compose -f deploy/docker-compose.yml build \
  --build-arg BUILDKIT_INLINE_CACHE=1 \
  <service>
```

当前 gascell 单机场景不需要，Docker 本地 layer cache 已足够。

---

## 4. 一键更新流程（核心脚本）

### 4.1 前置条件

在 gascell 服务器上执行：

```bash
# 确保在仓库根目录
cd /opt/hiaf-lab-system

# 确保 secrets 目录存在
ls deploy/secrets/db_password.txt deploy/secrets/jwt_key.txt \
   deploy/secrets/influxdb_token.txt deploy/secrets/agent_password.txt
```

### 4.2 完整更新脚本

脚本保存为 `C:\Users\47997\work\hiaf-lab-system\.hermes\update.sh`，内容如下：

```bash
#!/bin/bash
set -euo pipefail

# ============================================================
# hiaf-lab-system 一键更新脚本
# 用法: ./update.sh [--force] [--dry-run] [--no-rollback]
#   --force        跳过变更检测，全量重建
#   --dry-run      仅检测变更，不执行实际操作
#   --no-rollback  失败时不回滚（调试用）
# ============================================================

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

# ---- 步骤 1：预检 ----
log "===== 步骤 1/7：预检 ====="

if ! command -v docker &>/dev/null; then
  err "Docker 未安装或不在 PATH"; exit 1
fi
if ! docker compose version &>/dev/null; then
  err "docker compose (v2) 不可用"; exit 1
fi

AVAIL=$(df --output=avail -k "$REPO_ROOT" 2>/dev/null | tail -1)
if [ "${AVAIL:-0}" -lt 2097152 ]; then  # 2 GB
  warn "磁盘可用空间 < 2 GB，请清理后重试"
fi

# 确保 secrets 文件存在
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

# 备份数据库（可选，推荐）
BACKUP_FILE="/tmp/lab-db-backup-${OLD_SHORT}-$(date +%Y%m%d_%H%M%S).sql"
if [ "$DRY_RUN" = false ]; then
  log "备份数据库 → $BACKUP_FILE"
  docker compose -f "$COMPOSE_FILE" exec -T postgres \
    pg_dump -U lab lab > "$BACKUP_FILE" 2>/dev/null || {
    warn "数据库备份失败（postgres 未运行？），继续…"
    BACKUP_FILE=""
  }
fi

# ---- 步骤 3：拉取代码 ----
log "===== 步骤 3/7：git pull ====="

git fetch origin
BEFORE_PULL=$(git rev-parse HEAD)

if git pull --ff-only origin main 2>&1; then
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

detect_service() {
  # 返回需要重建的 service 名称列表
  local svc=""
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
  svc="${svc# }"  # trim leading space
  if [ -z "$svc" ]; then
    # 如果只有文档、.md 等无影响文件变更，可能无需重建
    # 但 migrations 变更总是要跑 migrate
    echo "$CHANGED_FILES" | grep -q 'migrations/' && echo "migrate" && return
    echo "none"
  else
    echo "$svc" | tr ' ' '\n' | sort -u | tr '\n' ' '
  fi
}

AFFECTED_SERVICES=$(detect_service | xargs)
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

# 全量重建模式
if echo "$AFFECTED_SERVICES" | grep -q "ALL"; then
  warn "compose 文件或 Dockerfile 变更，执行全量重建"
  docker compose -f "$COMPOSE_FILE" build --pull server py-agent py-agent-interpret epics-gateway ioc migrate
else
  # 按依赖顺序构建（独立服务并行，依赖服务串行）
  # 阶段 1: 独立服务（可并行）
  for svc in epics-gateway ioc py-agent-interpret; do
    if echo "$AFFECTED_LIST" | grep -q "$svc"; then
      log "构建 $svc …"
      docker compose -f "$COMPOSE_FILE" build "$svc"
      # py-agent-interpret 和 py-agent 共用镜像，只需构建一次
      if [ "$svc" = "py-agent-interpret" ]; then
        AFFECTED_SERVICES=$(echo "$AFFECTED_SERVICES" | sed 's/py-agent //')
      fi
    fi
  done

  # 阶段 2: server（依赖 epics-gateway、py-agent-interpret 已运行）
  if echo "$AFFECTED_LIST" | grep -q "server"; then
    log "构建 server（含前端）…"
    docker compose -f "$COMPOSE_FILE" build server
  fi

  # 阶段 3: py-agent（依赖 server 镜像，共享 py-agent-interpret 的镜像）
  if echo "$AFFECTED_LIST" | grep -q "py-agent"; then
    log "py-agent 与 py-agent-interpret 共用镜像，已构建。"
  fi
fi

# ---- 步骤 6：滚动重启 + 迁移 ----
log "===== 步骤 6/7：滚动更新 ====="

restart_service() {
  local svc=$1
  local health_check=$2  # 健康检查等待秒数
  log "重启 $svc …"
  docker compose -f "$COMPOSE_FILE" up -d --no-deps "$svc"
  sleep 2
  # 等待健康
  for i in $(seq 1 "$health_check"); do
    STATUS=$(docker compose -f "$COMPOSE_FILE" ps --format json "$svc" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('Health',''))" 2>/dev/null || echo "")
    if [ "$STATUS" = "healthy" ]; then
      log "$svc 健康 ($((i*2))s)"
      return 0
    fi
    # 也检查 running 状态（部分服务无 healthcheck）
    RUNNING=$(docker compose -f "$COMPOSE_FILE" ps --format json "$svc" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('State',''))" 2>/dev/null || echo "")
    if [ "$RUNNING" = "running" ]; then
      log "$svc 运行中"
      return 0
    fi
    sleep 2
  done
  err "$svc 健康检查超时"
  return 1
}

# --- 子步骤 6.1：先停掉依赖链末端的服务 ---
log "暂停 py-agent …"
docker compose -f "$COMPOSE_FILE" stop py-agent 2>/dev/null || true

log "暂停 server（保持 postgres + epics-gateway + py-agent-interpret 运行）…"
docker compose -f "$COMPOSE_FILE" stop server 2>/dev/null || true

# --- 子步骤 6.2：重启独立基础服务 ---
for svc in epics-gateway ioc py-agent-interpret; do
  if echo "$AFFECTED_LIST" | grep -q "$svc"; then
    restart_service "$svc" 30 || {
      err "$svc 重启失败"
      $NO_ROLLBACK || rollback
      exit 1
    }
  fi
done

# 确保 postgres 健康（它不应该变）
if ! docker compose -f "$COMPOSE_FILE" ps postgres | grep -q "(healthy)"; then
  warn "postgres 不健康，尝试重启…"
  docker compose -f "$COMPOSE_FILE" up -d postgres
  sleep 5
fi

# --- 子步骤 6.3：跑数据库迁移 ---
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

# --- 子步骤 6.4：重启 server ---
if echo "$AFFECTED_LIST" | grep -q "server"; then
  restart_service "server" 15 || {
    err "server 启动失败，查看日志:"
    docker compose -f "$COMPOSE_FILE" logs --tail 50 server
    $NO_ROLLBACK || rollback
    exit 1
  }
fi

# --- 子步骤 6.5：重启 py-agent ---
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
  local state
  state=$(docker compose -f "$COMPOSE_FILE" ps --format json "$svc" 2>/dev/null | python3 -c "
import sys,json
try:
  d=json.load(sys.stdin)
  print(d.get('Health', d.get('State', 'unknown')))
except: print('missing')
" 2>/dev/null || echo "missing")

  case "$state" in
    healthy|running)
      echo -e "  ${GREEN}✔${NC} $svc: $state" ;;
    *)
      echo -e "  ${RED}✘${NC} $svc: $state"
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
  # 发送成功通知
  curl -s -X POST http://localhost:8085/lab-system \
    -H "Title: 系统更新成功" \
    -H "Priority: default" \
    -H "Tags: white_check_mark" \
    -d "Updated ${OLD_SHORT} → ${NEW_SHORT}" >/dev/null 2>&1 || true
else
  err "部分服务不健康，请检查！"
  $NO_ROLLBACK || rollback
  exit 1
fi

# ---- 清理 ----
if [ -n "${BACKUP_FILE:-}" ] && [ -f "$BACKUP_FILE" ]; then
  log "数据库备份保留: $BACKUP_FILE"
fi

log "完成。"

# ============================================================
# 回滚函数
# ============================================================
rollback() {
  log ""
  warn "========== 开始回滚 =========="

  if [ -z "${OLD_SHA:-}" ]; then
    err "无旧 commit 记录，无法回滚。"
    exit 1
  fi

  # 1. 恢复代码
  log "恢复代码到 $OLD_SHORT …"
  git checkout "$OLD_SHA"

  # 2. 如果之前有数据库备份且有迁移变更，恢复数据库
  if [ -n "${BACKUP_FILE:-}" ] && [ -f "$BACKUP_FILE" ]; then
    if echo "${AFFECTED_SERVICES:-}" | grep -q "migrate"; then
      warn "检测到迁移变更，建议手动检查数据库: $BACKUP_FILE"
      # 不自动回滚数据库（危险操作），仅提示
    fi
  fi

  # 3. 用旧代码重建并重启
  log "用旧代码重建受影响服务 …"
  for svc in $AFFECTED_SERVICES; do
    [ "$svc" = "migrate" ] && continue  # migrate 是 one-shot，不重建
    docker compose -f "$COMPOSE_FILE" build "$svc"
    docker compose -f "$COMPOSE_FILE" up -d --no-deps "$svc"
  done

  # 4. 运行旧版 migrate（downgrade 风险由人工判断）
  if echo "${AFFECTED_SERVICES:-}" | grep -q "migrate"; then
    warn "迁移已执行。如数据库 schema 已变更，需手动 migrate down。"
    warn "查看迁移版本: docker compose -f $COMPOSE_FILE run --rm migrate -path /migrations -database ... version"
  fi

  # 5. 通知
  curl -s -X POST http://localhost:8085/lab-system \
    -H "Title: 系统更新失败-已回滚" \
    -H "Priority: urgent" \
    -H "Tags: warning" \
    -d "Rollback to $OLD_SHORT after update $NEW_SHORT failed" >/dev/null 2>&1 || true

  warn "========== 回滚完成 =========="
  warn "请检查服务状态并排查失败原因。"
  exit 1
}
```

### 4.3 部署脚本到 gascell 服务器

```bash
# 将脚本同步到服务器
scp .hermes/update.sh gascell:/opt/hiaf-lab-system/.hermes/update.sh
ssh gascell "chmod +x /opt/hiaf-lab-system/.hermes/update.sh"
```

### 4.4 一键更新别名（可选）

在 gascell 服务器的 `~/.bashrc` 中添加：

```bash
alias lab-update='cd /opt/hiaf-lab-system && ./.hermes/update.sh'
alias lab-update-dry='cd /opt/hiaf-lab-system && ./.hermes/update.sh --dry-run'
alias lab-update-force='cd /opt/hiaf-lab-system && ./.hermes/update.sh --force'
```

使用时在 gascell 上直接运行：

```bash
lab-update              # 标准更新：pull → 检测变更 → 增量构建 → 迁移 → 滚动重启
lab-update --dry-run    # 预览模式：仅显示变更文件和服务，不操作
lab-update --force      # 强制全量重建（即使无变更）
```

---

## 5. 回滚策略

### 自动回滚（脚本内置）

| 失败阶段 | 回滚动作 |
|----------|---------|
| git pull 失败 | 不执行任何操作，保持旧代码运行 |
| 构建失败 | 不重启任何容器，旧服务继续运行。手动修复后重试 |
| 迁移失败 | `git checkout` 回旧代码，旧服务继续运行。数据库 schema 未变 |
| server / py-agent 启动失败 | `git checkout` 回旧代码 → 重建该服务 → 重启。旧服务继续运行 |
| 健康检查失败 | 同上 |

### 数据库回滚（手动）

迁移失败时 `migrate` 容器不会修改数据库（golang-migrate 的事务特性）。但如果迁移已成功、后续步骤失败，数据库 schema 已变更。此时：

```bash
# 查看当前迁移版本
docker compose -f deploy/docker-compose.yml run --rm migrate \
  -path /migrations \
  -database "postgres://lab:$(cat deploy/secrets/db_password.txt)@postgres:5432/lab?sslmode=disable" \
  version

# 回退一个版本（需要 down.sql 存在）
docker compose -f deploy/docker-compose.yml run --rm migrate \
  -path /migrations \
  -database "postgres://lab:$(cat deploy/secrets/db_password.txt)@postgres:5432/lab?sslmode=disable" \
  down 1
```

**重要**：数据库自动回滚是危险操作。脚本只恢复代码 + 提示人工检查，不做自动 DB 回滚。

### 回滚测试建议

在 staging 环境先验证：

```bash
# 1. 人为注入一个启动失败的版本
git checkout <broken-sha>
./.hermes/update.sh --force

# 2. 确认回滚到 old-sha
git log --oneline -1

# 3. 确认服务恢复
docker compose -f deploy/docker-compose.yml ps
```

---

## 6. CI/CD 集成建议

### GitHub Actions 触发自动更新（可选）

```yaml
# .github/workflows/deploy.yml
name: Deploy to Gascell
on:
  push:
    branches: [main]
  workflow_dispatch:  # 手动触发

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger update on gascell
        uses: appleboy/ssh-action@v1
        with:
          host: ${{ secrets.GASCELL_HOST }}
          username: ${{ secrets.GASCELL_USER }}
          key: ${{ secrets.GASCELL_SSH_KEY }}
          script: |
            cd /opt/hiaf-lab-system
            ./.hermes/update.sh
```

**建议**：先用手动触发（`workflow_dispatch`），验证稳定后再启用 `push` 自动触发。保持手动触发 + ntfy 通知的双保险模式。

---

## 7. 常见问题

### Q: 如果 compose 文件也变了怎么办？

强制全量重建模式（`detect_service` 返回 `ALL`），所有镜像重建 + 全部容器重启。

### Q: 前端变了是否需要重建 Go？

是。前端通过 `//go:embed static` 嵌入 Go 二进制，前端变更 = server 镜像变更。

### Q: 如何只更新前端不重启后端？

当前架构做不到，前端和 Go 在同一个二进制中。如果要分离，需要将前端部署为独立 nginx 容器，这是架构层面的改动，不在本方案范围。

### Q: 构建太慢怎么办？

1. Docker layer cache 已优化（依赖层在前）。
2. 增量更新只构建变更的服务，大部分时间只构建 server。
3. 可在 gascell 本地配置 Docker registry mirror 加速基础镜像拉取。

### Q: 更新期间用户会看到什么？

- `server` 重启期间（~10~30s）：502 错误。
- 其他服务重启期间（epics-gateway、py-agent-interpret）：server 依赖它们，server 也会不可用。
- 优化方向：蓝色-绿色部署（需要额外 IP/端口），当前单机场景暂不实施。

### Q: 如何验证更新是否成功？

脚本最后输出全栈健康检查结果。也可以手动验证：

```bash
curl http://localhost:8000/health          # Go server
docker compose -f deploy/docker-compose.yml ps  # 全部容器状态
git log --oneline -1                        # 确认新版本
```

---

## 8. 文件清单

| 文件 | 说明 |
|------|------|
| `.hermes/update-system.md` | 本方案文档 |
| `.hermes/update.sh` | 一键更新脚本（部署到 gascell） |

### 待创建

`C:\Users\47997\work\hiaf-lab-system\.hermes\update.sh` — 按第 4.2 节脚本内容创建。
