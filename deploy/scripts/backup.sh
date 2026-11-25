#!/bin/bash
set -euo pipefail
# dump 含全库数据（含密码哈希），umask 077 确保备份文件 0600、目录 0700，禁止 0644 公开
umask 077

# ============================================================
# lab-backup.sh — 数据库每日定时备份（10h 优化 A 项）
#
# 用途：容器内 pg_dump 全量备份（自定义压缩格式 -Fc）到 /opt/lab-backups/lab-YYYYMMDD.dump，
#       保留 14 天（超出保留期的旧备份自动删除）；备份失败时经 SERVICE_TOKEN 通道
#       上报告警中心（POST /api/v1/alerts/report，由 alert 模块统一推送 ntfy）。
#
# 先例对照（本仓库既有实现，grep 核实后照抄）：
#   - pg_dump 容器内执行：.hermes/update.sh:175-183
#     （`docker compose -f "$COMPOSE_FILE" exec -T postgres pg_dump -U lab ...`）
#   - flock 并发锁：.hermes/update.sh:18-19
#   - 告警上报（SERVICE_TOKEN 通道 + 失败回退 ntfy 直发）：deploy/scripts/watchdog.sh:57-112
#   - compose 服务名：deploy/docker-compose.yml:2 `postgres:`（container_name lab-postgres）
#
# 部署（gascell 部署机，SELinux Enforcing，参照 deploy/scripts/README.md watchdog 先例）：
#   sudo install -m 755 deploy/scripts/backup.sh /usr/local/bin/lab-backup.sh
#   sudo restorecon -v /usr/local/bin/lab-backup.sh   # 恢复 bin_t context
#   配 systemd timer（deploy/systemd/lab-backup.{service,timer}）每日 03:00 执行。
#   注意：脚本安装到 /usr/local/bin 后，`$SCRIPT_DIR/../secrets/...` 相对路径失效，
#   须在 systemd service 里用 BACKUP_SERVICE_TOKEN_FILE / BACKUP_COMPOSE_FILE
#   环境变量显式指向仓库绝对路径（与 README watchdog 示例同款处理）。
#
# 手动执行（可重复执行，flock 防重入）：
#   bash deploy/scripts/backup.sh
#
# 可用 env 覆盖（便于测试/演练）：
#   BACKUP_DIR               备份目录（默认 /opt/lab-backups）
#   BACKUP_COMPOSE_FILE      docker-compose.yml 绝对路径（默认按脚本位置推导仓库路径）
#   BACKUP_API_URL           Go 后端地址（默认 http://10.144.144.12:8000）
#   BACKUP_SERVICE_TOKEN_FILE  service_token.txt 路径（默认仓库 deploy/secrets/）
#   BACKUP_NTFY_TOKEN_FILE   ntfy 发布 token 路径（告警中心不可用时的直发兜底）
#   BACKUP_RETENTION_DAYS    保留天数（默认 14）
#   BACKUP_LOCK_FILE         并发锁路径（默认 /var/lock/lab-backup.lock，非 root 手跑时可指 /tmp）
# ============================================================

# 并发锁：防 cron 与手动触发重入（update.sh:18-19 先例）
exec 9>"${BACKUP_LOCK_FILE:-/var/lock/lab-backup.lock}"
flock -n 9 || { echo "another backup in progress"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

BACKUP_DIR="${BACKUP_DIR:-/opt/lab-backups}"
COMPOSE_FILE="${BACKUP_COMPOSE_FILE:-$REPO_ROOT/deploy/docker-compose.yml}"
API_URL="${BACKUP_API_URL:-http://10.144.144.12:8000}"
SERVICE_TOKEN_FILE="${BACKUP_SERVICE_TOKEN_FILE:-$SCRIPT_DIR/../secrets/service_token.txt}"
NTFY_TOKEN_FILE="${BACKUP_NTFY_TOKEN_FILE:-$SCRIPT_DIR/../secrets/ntfy_publish_token.txt}"
NTFY_URL="${BACKUP_NTFY_URL:-http://127.0.0.1:8085/lab-alerts}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
INFLUX_RETENTION_DAYS="${BACKUP_INFLUX_RETENTION_DAYS:-7}"
UPLOADS_RETENTION_DAYS="${BACKUP_UPLOADS_RETENTION_DAYS:-30}"
INFLUX_TOKEN_FILE="${BACKUP_INFLUX_TOKEN_FILE:-$SCRIPT_DIR/../secrets/influxdb_token.txt}"
RCLONE_REMOTE="${BACKUP_RCLONE_REMOTE:-}"
DATE_TAG="$(date +%Y%m%d)"

fail_backup() {
  report_alert updater "数据库定时备份失败" "$1" "error"
  exit 1
}

# ---- 告警上报（照抄 watchdog.sh:84-112 的调用格式：SERVICE_TOKEN 通道 + ntfy 直发兜底）----
json_escape() {
  # JSON 字符串最小转义（标题/正文来自固定文案与文件路径，只兜底引号/反斜杠）
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

send_alert() {
  # ntfy 直发兜底（告警中心不可用时的双保险，与 watchdog.sh 同款）
  local title="$1" body="$2" priority="$3" tags="$4"
  local token=""
  if [ -f "$NTFY_TOKEN_FILE" ]; then
    token="$(tr -d '[:space:]' < "$NTFY_TOKEN_FILE")"
  fi
  local args=(
    -sS -m 5 -X POST "$NTFY_URL"
    -H "Title: $title"
    -H "Priority: $priority"
    -H "Tags: $tags"
    -H "Click: $API_URL/alerts"
    -d "$body"
  )
  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  curl "${args[@]}" >/dev/null 2>&1 || echo "警告：ntfy 告警发送失败（$title）" >&2
}

report_alert() {
  # 上报告警中心（POST /api/v1/alerts/report，SERVICE_TOKEN 鉴权）；失败回退直发 ntfy
  # $1 source $2 title $3 detail $4 level
  local source="$1" title="$2" detail="$3" level="$4"
  local token=""
  if [ -f "$SERVICE_TOKEN_FILE" ]; then
    token="$(tr -d '[:space:]' < "$SERVICE_TOKEN_FILE")"
  else
    echo "警告：service token 文件不存在（$SERVICE_TOKEN_FILE），告警中心不可用，回退 ntfy 直发" >&2
  fi
  if [ -z "$token" ] || ! curl -sf -m 5 -X POST "$API_URL/api/v1/alerts/report" \
      -H "Authorization: Bearer $token" \
      -H "Content-Type: application/json" \
      -d "$(printf '{"level":"%s","source":"%s","title":"%s","detail":"%s"}' \
        "$(json_escape "$level")" "$(json_escape "$source")" \
        "$(json_escape "$title")" "$(json_escape "$detail")")" >/dev/null 2>&1; then
    echo "警告：告警中心上报失败（$title），回退 ntfy 直发" >&2
    send_alert "$title" "$detail" "high" "warning"
  fi
}

# ---- 配置预检：compose 文件必须存在（/usr/local/bin 安装后靠 env 指向仓库，见 service 注释）----
if [ ! -f "$COMPOSE_FILE" ]; then
  report_alert updater "数据库定时备份失败" \
    "配置错误：compose 文件不存在（$COMPOSE_FILE），请检查 BACKUP_COMPOSE_FILE 环境变量" "error"
  exit 1
fi

# ---- 异常兜底：未在 if/|| 中显式处理的失败也上报告警（set -e 生效时触发）----
# 只清理 pg_dump 错误临时文件，不删已成功的 dump（避免 du/find 等收尾步骤失败误删有效备份）
err_trap_alert() {
  local code=$?
  [ -n "${PGDUMP_ERR:-}" ] && rm -f "$PGDUMP_ERR" 2>/dev/null
  report_alert updater "数据库定时备份失败" \
    "备份脚本异常退出（exit $code），请查看 journalctl -u lab-backup.service 或手动运行 backup.sh" "error"
}
trap err_trap_alert ERR

# ---- 备份 ----
mkdir -p "$BACKUP_DIR"

DUMP_FILE="$BACKUP_DIR/lab-$(date +%Y%m%d).dump"
PGDUMP_ERR="$(mktemp)"

echo "备份数据库 → $DUMP_FILE"
# 容器内 pg_dump（-Fc 压缩格式；compose 服务名 postgres，见 docker-compose.yml:2）
if ! docker compose -f "$COMPOSE_FILE" exec -T postgres \
    pg_dump -U lab -Fc lab > "$DUMP_FILE" 2>"$PGDUMP_ERR"; then
  ERR_TAIL="$(tail -n 3 "$PGDUMP_ERR" 2>/dev/null | tr '\n' ' ')"
  rm -f "$DUMP_FILE" "$PGDUMP_ERR"
  report_alert updater "数据库定时备份失败" \
    "pg_dump 失败，已删除残缺备份 $DUMP_FILE：${ERR_TAIL:-无错误输出}" "error"
  exit 1
fi
rm -f "$PGDUMP_ERR"

# ---- InfluxDB（AR-06）：influx backup 在线导出，目录整体拷到备份盘 ----
INFLUX_TOKEN=""
if [ -f "$INFLUX_TOKEN_FILE" ]; then
  INFLUX_TOKEN="$(tr -d '[:space:]' < "$INFLUX_TOKEN_FILE")"
fi
[ -n "$INFLUX_TOKEN" ] || fail_backup "InfluxDB token 缺失（$INFLUX_TOKEN_FILE）"

INFLUX_DIR="$BACKUP_DIR/influx-$DATE_TAG"
rm -rf "$INFLUX_DIR"
docker compose -f "$COMPOSE_FILE" exec -T influxdb rm -rf "/tmp/influx-$DATE_TAG" >/dev/null 2>&1 || true
echo "备份 InfluxDB → $INFLUX_DIR"
if ! docker compose -f "$COMPOSE_FILE" exec -T influxdb \
    influx backup "/tmp/influx-$DATE_TAG" --token "$INFLUX_TOKEN" >/dev/null 2>&1; then
  fail_backup "influx backup 失败（容器 lab-influxdb）"
fi
if ! docker cp "lab-influxdb:/tmp/influx-$DATE_TAG" "$INFLUX_DIR" >/dev/null 2>&1; then
  fail_backup "InfluxDB 备份目录拷贝失败"
fi
docker compose -f "$COMPOSE_FILE" exec -T influxdb rm -rf "/tmp/influx-$DATE_TAG" >/dev/null 2>&1 || true

# ---- 附件 volume（AR-06）：server 容器内打包 /app/uploads ----
UPLOADS_FILE="$BACKUP_DIR/uploads-$DATE_TAG.tar.gz"
echo "备份附件 → $UPLOADS_FILE"
if ! docker compose -f "$COMPOSE_FILE" exec -T server \
    tar czf - -C /app/uploads . > "$UPLOADS_FILE" 2>/dev/null; then
  rm -f "$UPLOADS_FILE"
  fail_backup "附件打包失败（容器 lab-server /app/uploads）"
fi

# ---- 运行时图片目录（AR-06）：仓库 images/ ----
IMAGES_FILE="$BACKUP_DIR/images-$DATE_TAG.tar.gz"
if [ -d "$REPO_ROOT/images" ]; then
  echo "备份图片 → $IMAGES_FILE"
  tar czf "$IMAGES_FILE" -C "$REPO_ROOT" images || fail_backup "images/ 打包失败"
else
  echo "跳过图片备份：$REPO_ROOT/images 不存在"
fi

# ---- 清单与校验和（AR-06）----
MANIFEST="$BACKUP_DIR/manifest-$DATE_TAG.txt"
{
  echo "date=$DATE_TAG"
  if [ -f "$DUMP_FILE" ]; then sha256sum "$DUMP_FILE"; fi
  if [ -f "$UPLOADS_FILE" ]; then sha256sum "$UPLOADS_FILE"; fi
  if [ -f "$IMAGES_FILE" ]; then sha256sum "$IMAGES_FILE"; fi
  if [ -d "$INFLUX_DIR" ]; then find "$INFLUX_DIR" -type f -exec sha256sum {} +; fi
} > "$MANIFEST"

# ---- 保留策略：DB 14 天、Influx 7 天、附件/图片 30 天 ----
find "$BACKUP_DIR" -maxdepth 1 -name 'lab-*.dump' -mtime +"$RETENTION_DAYS" -delete
find "$BACKUP_DIR" -maxdepth 1 -name 'uploads-*.tar.gz' -mtime +"$UPLOADS_RETENTION_DAYS" -delete
find "$BACKUP_DIR" -maxdepth 1 -name 'images-*.tar.gz' -mtime +"$UPLOADS_RETENTION_DAYS" -delete
find "$BACKUP_DIR" -maxdepth 1 -name 'manifest-*.txt' -mtime +"$UPLOADS_RETENTION_DAYS" -delete
find "$BACKUP_DIR" -maxdepth 1 -type d -name 'influx-*' -mtime +"$INFLUX_RETENTION_DAYS" -exec rm -rf {} +

# ---- 异地副本（AR-06）：配置 BACKUP_RCLONE_REMOTE 才启用 ----
if [ -n "$RCLONE_REMOTE" ]; then
  if ! command -v rclone >/dev/null 2>&1; then
    fail_backup "异地同步已配置但 rclone 未安装（$RCLONE_REMOTE）"
  fi
  echo "同步到异地 → $RCLONE_REMOTE"
  if ! rclone sync "$BACKUP_DIR" "$RCLONE_REMOTE"; then
    fail_backup "异地同步失败（本地备份已生成，$RCLONE_REMOTE）"
  fi
fi

SIZE="$(du -sh "$DUMP_FILE" 2>/dev/null | cut -f1)"
echo "备份完成：DB $DUMP_FILE（$SIZE）、Influx $INFLUX_DIR、附件 $UPLOADS_FILE、清单 $MANIFEST"
exit 0
