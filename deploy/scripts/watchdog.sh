#!/usr/bin/env bash
# watchdog.sh — 实验室服务心跳告警（C6：只告警，绝不自动重启）。
#
# 每次执行做一轮探测，由宿主机 cron / systemd timer 每 60s 触发一次
# （挂载方式见 deploy/scripts/README.md）。探测目标：
#   lab-server  http://10.144.144.12:8000/health （go-server，main.go 的 /health）
#   lab-ioc     http://10.144.144.12:5080/health  （EPICS 虚拟 IOC）
#
# 行为：
#   - 单服务连续 3 次失败（≈3 分钟）→ 发 ntfy 告警到 lab-alerts（标题含容器名）
#   - 恢复后发一条"已恢复"，连续失败计数清零
#   - 幂等 + 状态落盘：计数存 $STATE_DIR/<svc>.fail，告警态存 <svc>.alerted，
#     同一状态不重复发（失败期间只发一次，恢复只发一次）
#   - --dry-run：只打印探测结果与落盘状态，不告警、不写状态
#
# 明确不做自动重启：compose 的 restart: unless-stopped 已覆盖崩溃场景；
# "探测到不健康就 restart" 在仪器安全场景属于明确排除的自愈（方案 §1 C6 / §5）。
#
# 凭据：deploy/secrets/ntfy_publish_token.txt（todo-publisher 的 Bearer token，
# 由 deploy/scripts/init_ntfy.sh 生成，已授 lab-alerts write；严禁用
# service_token.txt——那是 Go 内部服务 token，ntfy 侧无该用户）。
#
# TODO（二期，未做）：docker inspect 探 postgres/influxdb/grafana/ntfy 容器健康态；
# IOC 心跳 PV（EPICS）探测，覆盖"进程活着但数据流断"场景。
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 可用 env 覆盖（便于测试/演练），默认值即生产配置
STATE_DIR="${WATCHDOG_STATE_DIR:-/tmp/watchdog_state}"
NTFY_URL="${WATCHDOG_NTFY_URL:-http://127.0.0.1:8085/lab-alerts}"
TOKEN_FILE="${WATCHDOG_NTFY_TOKEN_FILE:-$SCRIPT_DIR/../secrets/ntfy_publish_token.txt}"
WEB_URL="${WATCHDOG_WEB_URL:-http://10.144.144.12:8000}"
FAIL_THRESHOLD="${WATCHDOG_FAIL_THRESHOLD:-3}"
PROBE_TIMEOUT=5

DRY_RUN=0
[ "${1:-}" = "--dry-run" ] && DRY_RUN=1

# name|container|url
SERVICES=(
  "server|lab-server|http://10.144.144.12:8000/health"
  "ioc|lab-ioc|http://10.144.144.12:5080/health"
)

if [ "$DRY_RUN" -eq 0 ]; then
  mkdir -p "$STATE_DIR"
fi

send_alert() {
  # $1 标题（含容器名） $2 正文 $3 优先级 $4 tags
  local title="$1" body="$2" priority="$3" tags="$4"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] 将发送告警：$title — $body"
    return 0
  fi
  local token=""
  if [ -f "$TOKEN_FILE" ]; then
    token="$(tr -d '[:space:]' < "$TOKEN_FILE")"
  else
    echo "警告：ntfy token 文件不存在（$TOKEN_FILE），告警将以无凭据方式尝试（可能被 403 拒绝）" >&2
  fi
  local args=(
    -sS -m 5 -X POST "$NTFY_URL"
    -H "Title: $title"
    -H "Priority: $priority"
    -H "Tags: $tags"
    -H "Click: $WEB_URL/"
    -d "$body"
  )
  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  curl "${args[@]}" >/dev/null 2>&1 || echo "警告：ntfy 告警发送失败（$title）" >&2
}

read_fails() {
  local fail_file="$1" fails=0
  if [ -f "$fail_file" ]; then
    fails="$(cat "$fail_file" 2>/dev/null)"
    [[ "$fails" =~ ^[0-9]+$ ]] || fails=0
  fi
  echo "$fails"
}

check() {
  local name="$1" container="$2" url="$3"
  local fail_file="$STATE_DIR/$name.fail"
  local alert_file="$STATE_DIR/$name.alerted"
  local fails
  fails="$(read_fails "$fail_file")"

  if curl -sf -m "$PROBE_TIMEOUT" -o /dev/null "$url"; then
    if [ -f "$alert_file" ]; then
      send_alert "$container 已恢复" \
        "$container（$url）健康检查恢复正常。此前连续失败 $fails 次，未执行任何自动重启。" \
        "default" "white_check_mark"
      [ "$DRY_RUN" -eq 0 ] && rm -f "$alert_file"
    fi
    [ "$DRY_RUN" -eq 0 ] && echo 0 > "$fail_file"
    echo "$container: 正常（连续失败计数 $fails → 0）"
  else
    fails=$((fails + 1))
    [ "$DRY_RUN" -eq 0 ] && echo "$fails" > "$fail_file"
    if [ "$fails" -ge "$FAIL_THRESHOLD" ] && [ ! -f "$alert_file" ]; then
      send_alert "$container 健康检查失败" \
        "$container（$url）已连续 $fails 次探测失败（约 ${fails} 分钟）。容器 restart 策略已覆盖崩溃场景，本告警仅通知人工介入，不会自动重启。" \
        "high" "warning,rotating_light"
      [ "$DRY_RUN" -eq 0 ] && : > "$alert_file"
    fi
    echo "$container: 探测失败（连续 $fails/$FAIL_THRESHOLD）"
  fi
}

for entry in "${SERVICES[@]}"; do
  IFS='|' read -r name container url <<< "$entry"
  check "$name" "$container" "$url"
done

exit 0
