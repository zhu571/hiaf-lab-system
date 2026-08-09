# deploy/scripts

宿主机运维脚本（在容器外、宿主机上直接执行）。

## init_ntfy.sh

ntfy 账号/ACL 初始化（幂等）。创建 todo-publisher 账号并签发 `deploy/secrets/ntfy_publish_token.txt`，
配置 `lab-todos-*` / `lab-alerts` / `lab-system` 的 write ACL，批量同步 per-user 只读账号，
生成 `service_token.txt` 与 `agent_password.txt`（py-agent 以 agent@system 登录 Go 侧的密码，
缺省时生成，已有文件不覆盖）。前置：`docker compose up -d` 已启动。

```bash
cd deploy && ./scripts/init_ntfy.sh
```

## watchdog.sh

服务心跳告警（C6：只告警，绝不自动重启）。每次执行探测一轮 lab-server / lab-ioc 的
`/health`，单服务连续 3 次失败（≈3 分钟）发 ntfy 告警到 `lab-alerts`，恢复后发"已恢复"。
状态落盘 `/tmp/watchdog_state/`（幂等，同状态不重复发）。

冒烟（只报告不告警、不写状态）：

```bash
./scripts/watchdog.sh --dry-run
```

### 生产部署（gascell 部署机，SELinux Enforcing）

部署机仓库路径为 `/home/gascell/hiaf-lab-system`。SELinux Enforcing 下 systemd 无法直接执行
用户 home 目录里的脚本（`status=203/EXEC`），必须先安装到系统路径并恢复 context：

```bash
sudo install -m 755 /home/gascell/hiaf-lab-system/deploy/scripts/watchdog.sh /usr/local/bin/lab-watchdog.sh
sudo restorecon -v /usr/local/bin/lab-watchdog.sh   # 恢复 bin_t context
```

注意脚本内 `TOKEN_FILE` 默认值按脚本位置解析（`$SCRIPT_DIR/../secrets/...`），安装到
`/usr/local/bin` 后必须用 `WATCHDOG_NTFY_TOKEN_FILE` 环境变量指向仓库内的凭据文件
（见下方 cron / systemd 示例）。

### 宿主机挂载（二选一，60s 间隔）

cron（最短粒度 1 分钟，与设计的 60s 周期一致）：

```cron
* * * * * WATCHDOG_NTFY_TOKEN_FILE=/home/gascell/hiaf-lab-system/deploy/secrets/ntfy_publish_token.txt /usr/local/bin/lab-watchdog.sh >> /var/log/lab-watchdog.log 2>&1
```

systemd timer（`/etc/systemd/system/lab-watchdog.{service,timer}`）：

```ini
# lab-watchdog.service
[Unit]
Description=HIAF lab service watchdog (alert only)

[Service]
Type=oneshot
Environment=WATCHDOG_NTFY_TOKEN_FILE=/home/gascell/hiaf-lab-system/deploy/secrets/ntfy_publish_token.txt
ExecStart=/usr/local/bin/lab-watchdog.sh
```

```ini
# lab-watchdog.timer
[Unit]
Description=Run lab watchdog every 60s

[Timer]
OnCalendar=*-*-* *:*:00
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now lab-watchdog.timer
```

`Type=oneshot` 的 service 每次执行完即退出，`systemctl status lab-watchdog.service` 显示
`inactive (dead)` 属正常现象，timer 到点会再次拉起；历史探测输出用
`journalctl -u lab-watchdog.service` 查看。

注意：

- 告警凭据读 `deploy/secrets/ntfy_publish_token.txt`（先跑 `init_ntfy.sh` 生成；严禁用
  `service_token.txt`，那是 Go 内部服务 token，ntfy 侧无该用户）。
- 仓库路径若不在 `/home/gascell/hiaf-lab-system`，上述安装命令和环境变量中的路径需相应修改。
- `/tmp` 重启清空只影响失败计数，属可接受范围；如需保留可用 `WATCHDOG_STATE_DIR` 覆盖。

## 离线部署与镜像打包

gascell 部署机无外网，运行时镜像与构建期基础镜像需在有外网的机器上预拉取/打包后导入
（daocloud 镜像前缀用于内网可达的镜像源，goproxy.cn / 清华 pip 源 / npm registry 镜像
解决构建期的依赖下载）。

### 运行时镜像清单（与 docker-compose.yml 的 `image:` 引用逐一对应，含 P1-3/P2-2 新增）

```text
postgres:16-alpine                                        # postgres 服务（无前缀；离线环境需在 .env/daemon.json 配置镜像源）
docker.m.daocloud.io/influxdb:2-alpine                    # influxdb
binwiederhier/ntfy:v2.27.0                                # ntfy 服务 + deploy/Dockerfile ntfy-cli 构建阶段
docker.m.daocloud.io/grafana/grafana:11.1.0               # grafana（P1-3 锁版）
docker.m.daocloud.io/prom/prometheus:v2.53.0              # prometheus（P2-2 锁版）
```

### 构建期基础镜像清单（目标机 `docker compose build` 所需，已逐文件核对 FROM 行）

```text
binwiederhier/ntfy:v2.27.0            # deploy/Dockerfile:2（ntfy-cli 阶段，与运行时同镜像）
node:20-alpine                        # deploy/Dockerfile:5（前端构建）
golang:1.22-alpine                    # deploy/Dockerfile:14（Go 构建）
alpine:3.20                           # deploy/Dockerfile:29（server 运行阶段）
migrate/migrate:v4.17.0               # deploy/Dockerfile.migrate:1（迁移工具）
python:3.12-alpine                    # deploy/Dockerfile.migrate:3（迁移告警发送，含 curl）
python:3.11-slim                      # py-agent/Dockerfile:1、go-server/epics-gateway/Dockerfile:1
docker.m.daocloud.io/library/python:3.11-slim  # py-agent/ioc/Dockerfile:1（ioc 已用 daocloud 前缀）
```

### 导出 / 导入

```bash
# 有外网机器（或在首次部署前的主机）：
docker pull <上述全部镜像>
docker save -o lab-images.tar <上述全部镜像>          # 较大（GB 级），确认磁盘空间

# 传输（U 盘 / 内网 scp / 离线网闸）后目标机：
docker load -i lab-images.tar
```

### 本地构建镜像（server / py-agent / migrate / ioc / epics-gateway）

不走 save/load——目标机 `docker compose build` 构建，需内网可达的依赖源：

- Go module 代理：`deploy/Dockerfile:16` 已用 goproxy.cn，离线时改配内网代理（`GOPROXY`）。
- npm registry：前端构建需内网 npm 镜像源（web-ui 构建阶段）。
- pip 镜像源：`py-agent/ioc/Dockerfile:3` 已用清华源；`py-agent/Dockerfile:5` 需在构建时注入
  `PIP_INDEX_URL`（compose 未配置时默认走公网，离线环境构建会失败）。

### 升级流程

1. 先在有外网机器拉取新版本镜像并 `docker save` → 传输 → 目标机 `docker load`。
2. 仓库版本升级后 compose 指明确版本号（禁止 `latest`）→ `docker compose up -d`。
3. 校验：`docker images` 与上述清单 diff 为空；`docker compose ps` 全部 healthy。
4. 离线镜像包随升级一并更新至备份存储（见 docs/maintenance-strategy.md 备份/恢复章节）。
