# deploy/scripts

宿主机运维脚本（在容器外、宿主机上直接执行）。

## init_ntfy.sh

ntfy 账号/ACL 初始化（幂等）。创建 todo-publisher 账号并签发 `deploy/secrets/ntfy_publish_token.txt`，
配置 `lab-todos-*` / `lab-alerts` / `lab-system` 的 write ACL，批量同步 per-user 只读账号，
生成 `service_token.txt`。前置：`docker compose up -d` 已启动。

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

### 宿主机挂载（二选一，60s 间隔）

cron（最短粒度 1 分钟，与设计的 60s 周期一致）：

```cron
* * * * * /home/zhuhaofan/hiaf-lab-system/deploy/scripts/watchdog.sh >> /var/log/lab-watchdog.log 2>&1
```

systemd timer（`/etc/systemd/system/lab-watchdog.{service,timer}`）：

```ini
# lab-watchdog.service
[Unit]
Description=HIAF lab service watchdog (alert only)

[Service]
Type=oneshot
ExecStart=/home/zhuhaofan/hiaf-lab-system/deploy/scripts/watchdog.sh
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

注意：

- 告警凭据读 `deploy/secrets/ntfy_publish_token.txt`（先跑 `init_ntfy.sh` 生成）。
- 仓库路径若不在 `/home/zhuhaofan/hiaf-lab-system`，cron/timer 中的路径需相应修改。
- `/tmp` 重启清空只影响失败计数，属可接受范围；如需保留可用 `WATCHDOG_STATE_DIR` 覆盖。
