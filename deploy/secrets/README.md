# deploy/secrets — 密钥清单与轮换 runbook

> 本目录存放 Docker compose secrets（`deploy/docker-compose.yml` 的 `secrets:` 段逐文件引用，
> 容器内经 bind-mount 只读挂载到 `/run/secrets/<name>`）。`*.txt` 已 git-ignored 不入库
> （`.gitignore`），本 README 入库。
> 轮换原则：**先服务端后消费方**（参照 ntfy 鉴权部署先例 init_ntfy.sh：先在服务端
> 建号/签发，再同步消费方 token 文件）；旧值先备份到 `/opt/hiaf-lab-system/.hermes/backups`
> （本 runbook 自定备份目录；update.sh 的 DB 备份在 `/tmp` 或 `UPDATE_BACKUP_DIR`，两者互不干扰），
> 验证通过前不删除。

## 1. 密钥清单

本地仓库 `ls deploy/secrets/` 默认只有 3 个文件；`agent_password` / `grafana_admin_password` /
`ntfy_publish_token` / `service_token` 在 gascell 部署机由 `deploy/scripts/init_ntfy.sh` 生成
（幂等，缺省才生成，不覆盖已有文件）。

| 文件 | 用途 | 消费方（容器/进程） | 生成命令 | 生成来源 |
|------|------|----------------------|----------|----------|
| `db_password.txt` | postgres `lab` 用户密码 | postgres（首启 `POSTGRES_PASSWORD_FILE`）、server（`go-server/common/db.go:18`）、migrate（one-shot） | `openssl rand -hex 16` | 手工/部署初始化 |
| `jwt_key.txt` | Go JWT 签名密钥（access+refresh 共用） | server（`go-server/main.go:49`） | `openssl rand -hex 32` | 手工/部署初始化 |
| `influxdb_token.txt` | InfluxDB admin token（写时序库） | ioc（`INFLUX_TOKEN_FILE`）、server sensors（`common.ReadSecret` 同源）；须与 `.env` 的 `INFLUX_TOKEN` 同值（influxdb 首启 init / grafana provisioning / server `INFLUXDB_TOKEN` 用 .env） | `openssl rand -hex 32` | 手工/部署初始化 |
| `agent_password.txt` | py-agent 以 `agent@system` 登录 Go 侧；server 同值校验 py-agent-interpret 内部调用 | py-agent（`AGENT_PASSWORD_FILE`）、py-agent-interpret、server（`PY_AGENT_INTERNAL_TOKEN_FILE`，`ask/service.go:116`） | `openssl rand -base64 24 \| tr -d '\n'` | `init_ntfy.sh` §6 |
| `ntfy_publish_token.txt` | ntfy `todo-publisher` 的 Bearer token（`tk_*`，已授 lab-todos-\*/lab-alerts/lab-system write） | server notify、ioc（仪器域告警）、py-agent（死信告警）、migrate（失败通知） | `docker exec lab-ntfy ntfy token add --label=lab-todos-publish todo-publisher` | `init_ntfy.sh` §1/2 |
| `service_token.txt` | Go 内部服务 token（白名单仅 4 端点：GET daily-reports/by-date、POST ask/execute、POST alerts/report、alerts/resolve） | server（校验方，`middleware/service_token.go`）、ioc（告警上报）、py-agent / py-agent-interpret（ask 出站）、watchdog.sh（每次执行重读，无需重启） | `openssl rand -hex 32` | `init_ntfy.sh` §4 |
| `grafana_admin_password.txt` | Grafana admin 登录密码（P1-3 secret 化） | grafana（entrypoint 内 `cat` 注入 `GF_SECURITY_ADMIN_PASSWORD`，仅首次初始化生效） | `openssl rand -base64 24 \| tr -d '\n'` | `init_ntfy.sh` §7 |

## 2. 完整轮换示例：jwt_key（JWT_SECRET）

```bash
# 0) 前置：cd 到部署机仓库根（gascell: /home/gascell/hiaf-lab-system），确认 docker compose 正常
cd /home/gascell/hiaf-lab-system

# 1) 备份旧值（放 /opt 备份目录——严禁放 secrets 目录内：*.bak 不被 .gitignore 覆盖，会误入库）
sudo cp deploy/secrets/jwt_key.txt /opt/hiaf-lab-system/.hermes/backups/jwt_key.bak.$(date +%Y%m%d-%H%M%S)

# 2) 生成新值并收紧权限（写入方保持与旧文件一致的属主）
openssl rand -hex 32 > deploy/secrets/jwt_key.txt
sudo chmod 600 deploy/secrets/jwt_key.txt

# 3) 重启 server（secret 是 bind-mount 只读文件，restart 即重新读取；无需 --force-recreate。
#    想连依赖一起重建可用：docker compose -f deploy/docker-compose.yml up -d --force-recreate server）
docker compose -f deploy/docker-compose.yml restart server

# 4) 验证
docker compose -f deploy/docker-compose.yml ps server          # healthy
# 旧 token 请求应 401（jwt_key 是唯一签名密钥，access/refresh 一并失效）
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer <旧token>" http://10.144.144.12:8000/api/v1/auth/profile
# 新登录正常（web 端提示重新登录即可）
# 影响面收尾：py-agent 的 agent@system 会话同样失效，重启让其重新 login
docker compose -f deploy/docker-compose.yml restart py-agent py-agent-interpret

# 5) 回滚（验证失败时）：恢复备份 → restart server
sudo cp /opt/hiaf-lab-system/.hermes/backups/jwt_key.bak.<时间戳> deploy/secrets/jwt_key.txt
docker compose -f deploy/docker-compose.yml restart server
```

**影响面**：

- 全员 access/refresh token 立即失效，需重新登录（JWT 由 jwt_key 签名，全量失效；
  `users.token_version` 机制是"定向踢人"，与轮换 jwt_key 是两回事）
- py-agent / py-agent-interpret 的 agent@system 会话失效：api.py 收到 401 会先尝试
  refresh，refresh 同样 401 后抛错——须重启这两个容器重新 login（见步骤 4）
- 无数据丢失，审计日志不受影响；更新/迁移流程不受影响（不依赖 JWT）

## 3. 各密钥轮换细则

### 3.1 service_token（0.5h，影响面小）

```bash
openssl rand -hex 32 > deploy/secrets/service_token.txt
sudo chmod 600 deploy/secrets/service_token.txt
docker compose -f deploy/docker-compose.yml restart server          # 先服务端（校验方）
docker compose -f deploy/docker-compose.yml restart ioc py-agent py-agent-interpret  # 后消费方
```

影响面：todos scheduler 拉日报、alert 上报/resolve、ask/execute 短暂中断；watchdog.sh
每次执行重读 token 文件，无需处理。回滚：恢复备份 → 重启上述容器。

### 3.2 db_password（影响面最大，谨慎排期）

```bash
# 1) 生成新密码
NEW_DB_PW="$(openssl rand -hex 16)"
# 2) 服务端先行：postgres 内改密（当前连接用旧密码可执行）
docker compose -f deploy/docker-compose.yml exec -T postgres psql -U lab -d lab -c "ALTER USER lab WITH PASSWORD '$NEW_DB_PW'"
# 3) 写文件（与上面 SQL 用同一个值，不能两处不一致）
umask 077; printf '%s\n' "$NEW_DB_PW" > deploy/secrets/db_password.txt
# 4) 重启连库服务（先 server；migrate 是 one-shot，下次更新流程自动用新密码）
docker compose -f deploy/docker-compose.yml restart server
```

影响面：server 重启瞬间 DB 连接中断（毫秒级，会话由连接池重连）；migrate 容器下次
运行时才用到新密码。回滚：`ALTER USER` 改回旧值 + 恢复备份文件 + restart server。

### 3.3 agent_password

agent@system 的 DB 侧密码 hash 在 `users` 表，更新走 admin API
（`POST /api/v1/admin/users/{id}/reset-password`，main.go:257，admin-only + 审计 + Idempotency-Key）。
**注意：不能靠重跑 seed-agent 轮换**——seed-agent 只在用户不存在时创建
（`go-server/cmd/seed-agent/main.go` 已存在即返回），且它不是 compose 服务。
轮换顺序：先服务端（DB hash）后消费方（文件 + 重启）。

```bash
# 1) 生成新密码
NEW_AGENT_PW="$(openssl rand -base64 24 | tr -d '\n')"
# 2) 服务端先行：admin 登录 → 更新 agent@system 的 DB 密码 hash
#    （admin 密码用 read -s 交互输入，不落 shell 历史/脚本）
read -s -p 'admin 密码（仅本次会话使用）: ' ADMIN_PW; echo
CK="$(mktemp)"; trap 'rm -f "$CK"' EXIT
CSRF="$(curl -s -c "$CK" -X POST http://10.144.144.12:8000/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["csrf_token"])')"
AGENT_UID="$(curl -s -b "$CK" http://10.144.144.12:8000/api/v1/admin/users \
  | python3 -c 'import sys,json;d=json.load(sys.stdin)["data"];print([u["id"] for u in d if u["username"]=="agent@system"][0])')"
curl -s -b "$CK" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: rotate-agent-pw-$(date +%s)" \
  -X POST "http://10.144.144.12:8000/api/v1/admin/users/$AGENT_UID/reset-password" \
  -d "{\"new_password\":\"$NEW_AGENT_PW\"}" -o /dev/null -w 'reset-password HTTP %{http_code}\n'
# 3) 更新文件（与上一步同一个值）
umask 077; printf '%s' "$NEW_AGENT_PW" > deploy/secrets/agent_password.txt
# 4) 重启消费方（server 在 AutoConfigure 启动时读文件校验 interpret 内部调用；
#    py-agent / py-agent-interpret 用新密码重新登录）
docker compose -f deploy/docker-compose.yml restart server py-agent py-agent-interpret
```

影响面：Agent 任务解析/执行短暂中断；agent@system 重新登录换取新 JWT；ask/chat 与
日志 AI 解析在 py-agent-interpret 重启完成前返回 502 `upstream_error`（AGENTS.md P0-3 降级）。
回滚：admin API 重置回旧密码 hash + 恢复文件 + 重启 server/py-agent/py-agent-interpret。

### 3.4 ntfy_publish_token（服务端=ntfy，先服务端后消费方）

```bash
# 1) ntfy 服务端签发新 token（先不动文件，服务端先有值）
docker exec lab-ntfy ntfy token list
NEW_NTFY_TK="$(docker exec lab-ntfy ntfy token add --label=lab-todos-publish-rotated todo-publisher | awk '/^tk_/{print $1; exit}')"
# 2) 更新文件
printf '%s' "$NEW_NTFY_TK" > deploy/secrets/ntfy_publish_token.txt
# 3) 重启消费方
docker compose -f deploy/docker-compose.yml restart server ioc py-agent
# 4) 确认通知正常后，服务端吊销旧 token
docker exec lab-ntfy ntfy token revoke <旧tk_>
```

影响面：通知类功能短暂中断（todo 提醒/告警/更新通知）；migrate 一次性容器下次运行
读新文件。回滚：文件恢复旧 token + 重启消费方（旧 token 未吊销前仍有效）。

### 3.5 influxdb_token（最复杂：DB 内部 + 文件 + .env 三处）

influxdb 的 admin token 存储在其内部（bolt 库），`DOCKER_INFLUXDB_INIT_ADMIN_TOKEN`
仅首启 setup 生效；且 `influxdb_token.txt` 与 `.env` 的 `INFLUX_TOKEN` 必须同值
（grafana provisioning 读 .env；server sensors 优先读 secrets 文件、env `INFLUXDB_TOKEN`
仅兜底，`go-server/sensors/service.go:64`；ioc 读 secrets 文件）。

```bash
# 1) 服务端：influx 内签发新的 all-access token
docker compose -f deploy/docker-compose.yml exec -T influxdb influx auth create --org lab-org --description rotated-admin --all-access --json
# 2) 同步两处文件：secrets 文件 + .env INFLUX_TOKEN（同值！）
# 3) 重启消费方
docker compose -f deploy/docker-compose.yml restart ioc server grafana
# 4) 验证（Grafana 数据源 / 时序写入正常）后吊销旧 token
docker compose -f deploy/docker-compose.yml exec -T influxdb influx auth find | grep <旧ID>   # 找到后 influx auth revoke
```

影响面：传感器时序写入、Grafana 查询短暂中断。回滚：恢复文件 + .env + 重签旧 token。

### 3.6 grafana_admin_password

entrypoint 注入的 `GF_SECURITY_ADMIN_PASSWORD` 只对**首次初始化**生效；已运行实例的密码
在 grafana.db，须用 grafana-cli 同步改：

```bash
# 1) 服务端：grafana-cli 改密码（新密码与服务端一致后写文件）
docker compose -f deploy/docker-compose.yml exec grafana grafana-cli admin reset-admin-password '<新密码>'
# 2) 更新文件
umask 077; printf '%s' '<新密码>' > deploy/secrets/grafana_admin_password.txt
# 3) 验证登录后无需重启（下次重建容器时文件值 = DB 值，不会漂移）
```

影响面：仅 Grafana 登录；重登一次。回滚：grafana-cli 改回旧值 + 恢复文件。

## 4. 轮换前检查清单

- [ ] 备份旧值到 `/opt/hiaf-lab-system/.hermes/backups/`（**不要**放 secrets 目录内，
      `*.bak` 不会被 `.gitignore` 忽略，会误入库）
- [ ] 确定消费方清单（见第 1 节表格），先服务端后消费方滚动
- [ ] 写文件后 `chmod 600`，属主与旧文件一致（server 容器以 UID 1000 读取）
- [ ] 验证通过再吊销/清理旧值（ntfy token、influx auth 等）
- [ ] 轮换动作本身在审计日志/告警中心留痕（可上报一条 watchdog 来源的告警说明轮换）
- [ ] 不在 secrets 目录内留任何非 .gitkeep 文件（除 *.txt 外全部会被 git 跟踪）
