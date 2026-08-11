#!/usr/bin/env bash
# watchdog.sh — 实验室服务心跳告警（C6：只告警，绝不自动重启）。
#
# 每次执行做一轮探测，由宿主机 cron / systemd timer 每 60s 触发一次
# （挂载方式见 deploy/scripts/README.md）。探测目标：
#   1. HTTP 探测（进程活着 + 端口响应）：
#      lab-server  http://10.144.144.12:8000/health （go-server，main.go 的 /health）
#      lab-ioc     http://10.144.144.12:5080/health  （EPICS 虚拟 IOC）
#   2. 全栈容器健康探测（10h 优化 L，docker inspect）：lab-postgres/lab-influxdb/
#      lab-grafana/lab-ioc/lab-server/lab-py-agent/lab-py-agent-interpret/lab-ntfy/
#      lab-prometheus 的 Up/healthy 状态；lab-migrate（one-shot，Exited 正常）不探测，
#      lab-prometheus 未启用时（容器不存在或 Exited）视为正常排除
#
# 行为：
#   - 单服务连续 3 次失败（≈3 分钟）→ 先上报告警中心（POST /api/v1/alerts/report，
#     由 alert 模块统一推送 ntfy）；上报失败回退脚本直发 ntfy 到 lab-alerts（双保险）
#   - 恢复后调 /api/v1/alerts/resolve 幂等解除对应 active 告警（恢复消息不再直发 ntfy），
#     连续失败计数清零
#   - 幂等 + 状态落盘：计数存 $STATE_DIR/<svc>.fail，告警态存 <svc>.alerted，
#     同一状态不重复报（失败期间只报一次，恢复只 resolve 一次）
#   - --dry-run：只打印探测结果与将上报的 report/resolve 内容，不落盘、不发送
#
# 明确不做自动重启：compose 的 restart: unless-stopped 已覆盖崩溃场景；
# "探测到不健康就 restart" 在仪器安全场景属于明确排除的自愈（方案 §1 C6 / §5）。
#
# 凭据（两个 token 并存，用途不同，方案 §8.1 #13）：
#   deploy/secrets/service_token.txt     —— 调 Go 内部 API（alerts/report、alerts/resolve）
#   deploy/secrets/ntfy_publish_token.txt—— ntfy 发布（todo-publisher 的 Bearer token，
#        由 deploy/scripts/init_ntfy.sh 生成，已授 lab-alerts write；严禁用 service_token
#        发 ntfy——ntfy 侧无该用户）。service_token.txt 缺失时告警中心不可用，
#        但脚本 ntfy 直发兜底仍可用（回退双保险）。
#
# TODO（二期，未做）：IOC 心跳 PV（EPICS）探测，覆盖"进程活着但数据流断"场景。
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 可用 env 覆盖（便于测试/演练），默认值即生产配置
STATE_DIR="${WATCHDOG_STATE_DIR:-/tmp/watchdog_state}"
NTFY_URL="${WATCHDOG_NTFY_URL:-http://127.0.0.1:8085/lab-alerts}"
TOKEN_FILE="${WATCHDOG_NTFY_TOKEN_FILE:-$SCRIPT_DIR/../secrets/ntfy_publish_token.txt}"
API_URL="${WATCHDOG_API_URL:-http://10.144.144.12:8000}"
SERVICE_TOKEN_FILE="${WATCHDOG_SERVICE_TOKEN_FILE:-$SCRIPT_DIR/../secrets/service_token.txt}"
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

# name|container（全栈容器健康探测，docker inspect）
# 排除项：lab-migrate 是 one-shot 容器（Exited 正常）不探测；
#         lab-prometheus 未启用时（容器不存在或 Exited）视为正常，不告警。
# 注：py-agent-interpret 未设 container_name（deploy/docker-compose.yml），
#     compose 自动命名为 <project>-py-agent-interpret-1（生产项目名=deploy），
#     由 resolve_container() 在探测前回退解析，避免名称漂移持续误报。
CONTAINERS=(
  "container-postgres|lab-postgres"
  "container-influxdb|lab-influxdb"
  "container-grafana|lab-grafana"
  "container-ioc|lab-ioc"
  "container-server|lab-server"
  "container-py-agent|lab-py-agent"
  "container-py-agent-interpret|lab-py-agent-interpret"
  "container-ntfy|lab-ntfy"
  "container-prometheus|lab-prometheus"
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
    -H "Click: $WEB_URL/alerts"
    -d "$body"
  )
  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  curl "${args[@]}" >/dev/null 2>&1 || echo "警告：ntfy 告警发送失败（$title）" >&2
}

json_escape() {
  # JSON 字符串最小转义（标题/正文来自容器名与探测 URL，无控制字符；只兜底引号/反斜杠）
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

report_alert() {
  # 上报告警中心（POST /api/v1/alerts/report，SERVICE_TOKEN 鉴权）；失败回退直发 ntfy
  # $1 source $2 title $3 detail $4 level
  local source="$1" title="$2" detail="$3" level="$4"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] 将上报告警中心：[$level/$source] $title — $detail"
    return 0
  fi
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

resolve_alert() {
  # 恢复后幂等解除告警中心 active 告警（POST /api/v1/alerts/resolve，source+title 匹配）
  # $1 source $2 title
  local source="$1" title="$2"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] 将解除告警中心：[${source}] $title"
    return 0
  fi
  local token=""
  if [ -f "$SERVICE_TOKEN_FILE" ]; then
    token="$(tr -d '[:space:]' < "$SERVICE_TOKEN_FILE")"
  fi
  if [ -z "$token" ]; then
    echo "警告：service token 文件不存在（$SERVICE_TOKEN_FILE），跳过告警中心 resolve" >&2
    return 0
  fi
  curl -sf -m 5 -X POST "$API_URL/api/v1/alerts/resolve" \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -d "$(printf '{"source":"%s","title":"%s"}' "$(json_escape "$source")" "$(json_escape "$title")")" \
    >/dev/null 2>&1 || echo "警告：告警中心 resolve 失败（$title）" >&2
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
      resolve_alert watchdog "$container 健康检查失败"
      [ "$DRY_RUN" -eq 0 ] && rm -f "$alert_file"
    fi
    [ "$DRY_RUN" -eq 0 ] && echo 0 > "$fail_file"
    echo "$container: 正常（连续失败计数 $fails → 0）"
  else
    fails=$((fails + 1))
    [ "$DRY_RUN" -eq 0 ] && echo "$fails" > "$fail_file"
    if [ "$fails" -ge "$FAIL_THRESHOLD" ] && [ ! -f "$alert_file" ]; then
      report_alert watchdog "$container 健康检查失败" \
        "$container（$url）已连续 $fails 次探测失败（约 ${fails} 分钟）。容器 restart 策略已覆盖崩溃场景，本告警仅通知人工介入，不会自动重启。" \
        "warning"
      [ "$DRY_RUN" -eq 0 ] && : > "$alert_file"
    fi
    echo "$container: 探测失败（连续 $fails/$FAIL_THRESHOLD）"
  fi
}

for entry in "${SERVICES[@]}"; do
  IFS='|' read -r name container url <<< "$entry"
  check "$name" "$container" "$url"
done

resolve_container() {
  # 容器名解析：lab-* 服务均有 container_name，直接命中；对未设 container_name 的服务
  # （py-agent-interpret），按 compose 自动命名规则 <project>-<service>-N 回退，
  # 找不到时保持原名（由调用方按 missing 处理，不会静默跳过一个真故障）。
  # $1 配置的容器名
  local want="$1" found
  if docker inspect -f '{{.Name}}' "$want" >/dev/null 2>&1; then
    echo "$want"
    return 0
  fi
  found="$(docker ps -a --format '{{.Names}}' | grep -E "^[A-Za-z0-9_.-]+-${want#lab-}-[0-9]+$" | head -1)"
  if [ -n "$found" ]; then
    echo "$found"
    return 0
  fi
  echo "$want"
}

check_container() {
  # 全栈容器健康探测（10h 优化 L）：docker inspect 探 Up/healthy 状态。
  # 状态机与 check() 一致：连续 $FAIL_THRESHOLD 次异常 → 上报告警中心，恢复后 resolve。
  # $1 逻辑名 $2 容器名
  local name="$1" container="$2"
  container="$(resolve_container "$container")"
  local fail_file="$STATE_DIR/$name.fail"
  local alert_file="$STATE_DIR/$name.alerted"
  local fails status health
  fails="$(read_fails "$fail_file")"

  status="$(docker inspect -f '{{.State.Status}}' "$container" 2>/dev/null | tr -d '[:space:]' || true)"
  [ -n "$status" ] || status="missing"
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container" 2>/dev/null | tr -d '[:space:]' || true)"
  [ -n "$health" ] || health="none"

  # 排除项：prometheus 未启用（容器不存在或 Exited）视为正常，不告警不计数；
  # 若此前曾告警（如运行中变 unhealthy 后停用），此处同步幂等解除，避免告警中心残留
  if [ "$container" = "lab-prometheus" ] && { [ "$status" = "missing" ] || [ "$status" = "exited" ]; }; then
    echo "$container: 已排除（状态 $status，未启用不告警）"
    if [ -f "$alert_file" ]; then
      resolve_alert watchdog "$container 容器状态异常"
      [ "$DRY_RUN" -eq 0 ] && rm -f "$alert_file"
    fi
    [ "$DRY_RUN" -eq 0 ] && echo 0 > "$fail_file"
    return 0
  fi

  if [ "$status" = "running" ] && { [ "$health" = "none" ] || [ "$health" = "healthy" ]; }; then
    if [ -f "$alert_file" ]; then
      resolve_alert watchdog "$container 容器状态异常"
      [ "$DRY_RUN" -eq 0 ] && rm -f "$alert_file"
    fi
    [ "$DRY_RUN" -eq 0 ] && echo 0 > "$fail_file"
    echo "$container: 容器正常（状态 $status，健康 $health，连续失败计数 $fails → 0）"
  else
    fails=$((fails + 1))
    [ "$DRY_RUN" -eq 0 ] && echo "$fails" > "$fail_file"
    if [ "$fails" -ge "$FAIL_THRESHOLD" ] && [ ! -f "$alert_file" ]; then
      report_alert watchdog "$container 容器状态异常" \
        "$container 状态=$status，健康=$health，已连续 $fails 次探测异常（约 ${fails} 分钟）。容器 restart 策略已覆盖崩溃场景，本告警仅通知人工介入，不会自动重启。" \
        "warning"
      [ "$DRY_RUN" -eq 0 ] && : > "$alert_file"
    fi
    echo "$container: 容器异常（状态 $status，健康 $health，连续 $fails/$FAIL_THRESHOLD）"
  fi
}

for entry in "${CONTAINERS[@]}"; do
  IFS='|' read -r name container <<< "$entry"
  check_container "$name" "$container"
done

exit 0
