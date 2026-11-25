#!/bin/bash
set -euo pipefail

# ============================================================
# lab-init-db-roles.sh — 应用账号最小权限（AR-05）
#
# 目标：
#   - lab_app：Go server 使用的登录角色，只有 DML + 序列，无 DDL；
#   - audit_log 对 lab_app 回收 UPDATE/DELETE/TRUNCATE（append-only 由 DB 强制）；
#   - 新表通过 ALTER DEFAULT PRIVILEGES 自动授权，避免漏 GRANT；
#   - ask_reader 只读角色授权给 lab_app（SET LOCAL ROLE 需要）。
#
# 幂等：可重复执行；角色存在则只更新密码/权限。
#
# 用法（在部署机仓库根目录）：
#   bash deploy/scripts/init_db_roles.sh
#
# 切换 server 到 lab_app（需要一次重启窗口；opt-in override）：
#   1) docker compose -f deploy/docker-compose.yml up -d postgres
#   2) bash deploy/scripts/init_db_roles.sh   # 本脚本生成 deploy/secrets/db_app_password.txt
#   3) docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.lab-app.yml up -d server
#   4) 跑登录/日志/审批/ask/附件/scans 冒烟；
#   5) 回滚：docker compose -f deploy/docker-compose.yml up -d server（DB_USER 回到 lab）。
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$REPO_ROOT/deploy/docker-compose.yml}"
SECRETS_DIR="${SECRETS_DIR:-$REPO_ROOT/deploy/secrets}"
APP_PASSWORD_FILE="${DB_APP_PASSWORD_FILE:-$SECRETS_DIR/db_app_password.txt}"

if [ ! -f "$APP_PASSWORD_FILE" ]; then
  umask 077
  openssl rand -hex 24 > "$APP_PASSWORD_FILE"
  echo "已生成应用账号密码：$APP_PASSWORD_FILE"
fi
APP_PASSWORD="$(tr -d '[:space:]' < "$APP_PASSWORD_FILE")"
[ -n "$APP_PASSWORD" ] || { echo "失败：$APP_PASSWORD_FILE 为空" >&2; exit 1; }

psql_exec() {
  docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U lab -d lab -v ON_ERROR_STOP=1 "$@"
}

echo "初始化 lab_app 角色与最小权限 …"
psql_exec -v app_password="$APP_PASSWORD" <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'lab_app') THEN
    CREATE ROLE lab_app LOGIN;
  END IF;
END $$;

ALTER ROLE lab_app WITH LOGIN PASSWORD :'app_password';

GRANT CONNECT ON DATABASE lab TO lab_app;
GRANT USAGE ON SCHEMA public TO lab_app;
REVOKE CREATE ON SCHEMA public FROM lab_app;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO lab_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO lab_app;

ALTER DEFAULT PRIVILEGES FOR ROLE lab IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO lab_app;
ALTER DEFAULT PRIVILEGES FOR ROLE lab IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO lab_app;

-- 审计 append-only：DB 层禁止改删（应用仍可 INSERT/SELECT）。
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM lab_app;

-- ask 只读角色：SET LOCAL ROLE ask_reader 需要成员关系（迁移 033 创建）。
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ask_reader') THEN
    EXECUTE 'GRANT ask_reader TO lab_app';
  END IF;
END $$;

-- 启动自检辅助：列出 lab_app 仍可写的表（应只包含业务表，不含 audit_log 的 UPDATE）。
SELECT grantee, table_name, privilege_type
FROM information_schema.role_table_grants
WHERE grantee = 'lab_app' AND privilege_type IN ('UPDATE', 'DELETE')
ORDER BY table_name, privilege_type;
SQL

echo "完成。验证（应失败）："
echo "  docker compose -f $COMPOSE_FILE exec -T postgres psql -U lab_app -d lab -c 'CREATE TABLE should_fail(id int);'"
echo "  docker compose -f $COMPOSE_FILE exec -T postgres psql -U lab_app -d lab -c 'UPDATE audit_log SET action=action;'"
echo "切换步骤见脚本头部注释；默认部署仍使用 lab，确认冒烟后再切 DB_USER。"
