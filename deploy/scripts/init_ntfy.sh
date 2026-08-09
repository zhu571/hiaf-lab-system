#!/usr/bin/env bash
# ntfy 账号/ACL 初始化（幂等，可在宿主机重复执行）：
#   1. 建 todo-publisher 账号 + 签发不过期 access token → deploy/secrets/ntfy_publish_token.txt
#   2. 通配 ACL：todo-publisher 对 lab-todos-*、lab-alerts 与 lab-system 有 write 权限
#      （server.yml 为 auth-default-access: deny-all，lab-system 是 agent 候选/更新通知主题，
#        无显式 ACL 时通知会被 403 静默丢弃）
#   3. 批量建 per-user 账号 todo-{username} + 各自 lab-todos-{sha256(user_id)[:16]} read-only ACL
#   4. 生成 service_token（todos scheduler 拉日报用）
#   5. 修正 /etc/ntfy 属主为 server 容器 UID（默认 1000，与 compose UPDATE_RUN_UID 对齐）
#   6. 生成 agent_password（py-agent 以 agent@system 登录 Go 侧）
#
# 用法：cd deploy && ./scripts/init_ntfy.sh
# 前置：docker compose up -d 已启动（lab-ntfy、lab-postgres 在跑）。
set -euo pipefail

NTFY_CONTAINER="lab-ntfy"
PG_CONTAINER="lab-postgres"
SECRETS_DIR="$(cd "$(dirname "$0")/.." && pwd)/secrets"
RUN_UID="${UPDATE_RUN_UID:-1000}"
RUN_GID="${UPDATE_RUN_GID:-1000}"
mkdir -p "$SECRETS_DIR"

# ---------- 等待 ntfy auth.db 就绪（ntfy serve 首启自动创建） ----------
for _ in $(seq 1 30); do
  if docker exec "$NTFY_CONTAINER" test -f /etc/ntfy/auth.db 2>/dev/null; then
    break
  fi
  sleep 2
done
docker exec "$NTFY_CONTAINER" test -f /etc/ntfy/auth.db || {
  echo "错误：ntfy auth.db 未就绪，请确认 lab-ntfy 已启动（docker compose up -d）" >&2
  exit 1
}

ntfy_user_exists() {
  docker exec "$NTFY_CONTAINER" ntfy user list 2>/dev/null | grep -qx "$1"
}

run_ntfy_env() {
  # 密码经 env-file 传入，避免出现在 docker exec argv/ps
  local env_file
  env_file="$(mktemp)"
  printf 'NTFY_PASSWORD=%s\n' "$1" > "$env_file"
  shift
  docker exec --env-file "$env_file" "$NTFY_CONTAINER" ntfy "$@"
  rm -f "$env_file"
}

# ---------- 1/2. publish 账号 + token + 通配 ACL ----------
if ! ntfy_user_exists "todo-publisher"; then
  run_ntfy_env "$(openssl rand -base64 24 | tr -d '\n')" user add todo-publisher
  echo "已创建 todo-publisher"
fi
docker exec "$NTFY_CONTAINER" ntfy access todo-publisher 'lab-todos-*' write >/dev/null 2>&1 || \
  docker exec "$NTFY_CONTAINER" ntfy access todo-publisher 'lab-todos-*' write
docker exec "$NTFY_CONTAINER" ntfy access todo-publisher lab-alerts write >/dev/null 2>&1 || \
  docker exec "$NTFY_CONTAINER" ntfy access todo-publisher lab-alerts write
# lab-system：agent 候选待审核 / 系统更新通知主题（deny-all 下无 ACL 即 403 静默，C11 修复）
docker exec "$NTFY_CONTAINER" ntfy access todo-publisher lab-system write >/dev/null 2>&1 || \
  docker exec "$NTFY_CONTAINER" ntfy access todo-publisher lab-system write

if [ ! -s "$SECRETS_DIR/ntfy_publish_token.txt" ]; then
  TOKEN="$(docker exec "$NTFY_CONTAINER" ntfy token add --label=lab-todos-publish todo-publisher | awk '/^tk_/{print $1; exit}')"
  if [ -z "$TOKEN" ]; then
    echo "错误：未取得 todo-publisher token（可先手工执行 ntfy token add todo-publisher 查看输出格式）" >&2
    exit 1
  fi
  printf '%s' "$TOKEN" > "$SECRETS_DIR/ntfy_publish_token.txt"
  chmod 600 "$SECRETS_DIR/ntfy_publish_token.txt"
  echo "已签发 publish token → $SECRETS_DIR/ntfy_publish_token.txt"
fi

# ---------- 3. 批量 per-user 账号 + read-only ACL（幂等） ----------
docker exec "$PG_CONTAINER" pg_isready -U lab -d lab >/dev/null 2>&1 || {
  echo "警告：lab-postgres 未就绪，跳过批量建号（运行时 ensureACL 会兜底）" >&2
}
if docker exec "$PG_CONTAINER" pg_isready -U lab -d lab >/dev/null 2>&1; then
  docker exec "$PG_CONTAINER" psql -U lab -d lab -t -A -F '|' \
    -c "SELECT id, username FROM users WHERE disabled = false" | while IFS='|' read -r uid username; do
    [ -z "$uid" ] && continue
    ntfy_user="todo-${username}"
    if ! ntfy_user_exists "$ntfy_user"; then
      run_ntfy_env "$(openssl rand -base64 24 | tr -d '\n')" user add "$ntfy_user" || true
    fi
    topic="lab-todos-$(printf '%s' "$uid" | sha256sum | cut -c1-16)"
    docker exec "$NTFY_CONTAINER" ntfy access "$ntfy_user" "$topic" read-only >/dev/null 2>&1 || true
  done
  echo "per-user 账号/ACL 批量同步完成"
fi

# ---------- 4. service token（todos scheduler 经 by-date 拉日报） ----------
if [ ! -s "$SECRETS_DIR/service_token.txt" ]; then
  openssl rand -hex 32 > "$SECRETS_DIR/service_token.txt"
  chmod 600 "$SECRETS_DIR/service_token.txt"
  echo "已生成 service token → $SECRETS_DIR/service_token.txt"
fi

# ---------- 5. auth-file 属主：server 容器以非 root（UID 1000）运行 ntfy CLI，
# 需对 /etc/ntfy 有写权限（WAL/SHM 文件也在该目录）；ntfy 服务以 root 运行不受影响。
docker exec "$NTFY_CONTAINER" chown -R "$RUN_UID:$RUN_GID" /etc/ntfy
echo "auth-file 属主已对齐 server 容器 UID $RUN_UID:$RUN_GID"

# ---------- 6. agent 登录密码（py-agent 以 agent@system 登录 Go 侧） ----------
# 只补缺省、不覆盖已有文件（已部署系统重跑脚本不会破坏 agent 登录）。
# agent@system 的 DB 侧密码哈希由 seed-agent 首启时写入，若手工重置该密码需同步重跑 seed。
if [ ! -s "$SECRETS_DIR/agent_password.txt" ]; then
  openssl rand -base64 24 | tr -d '\n' > "$SECRETS_DIR/agent_password.txt"
  chmod 600 "$SECRETS_DIR/agent_password.txt"
  echo "已生成 agent password → $SECRETS_DIR/agent_password.txt"
fi

echo "ntfy 初始化完成。"
