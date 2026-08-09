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
