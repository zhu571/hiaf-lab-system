# AR-06b 宿主侧更新执行器部署包

> 设计见 `docs/update-runner-hardening.md`。本目录提供 systemd 单元与 dispatcher，
> compose override 在 `deploy/docker-compose.host-update.yml`。

## 1. 前置

- 仓库在 `/opt/hiaf-lab-system`（或同步修改单元里的路径）。
- Docker Compose >= 2.24.4。
- 创建专用用户：

```bash
sudo useradd --system --home /opt/hiaf-lab-system --shell /usr/sbin/nologin lab-update
sudo usermod -aG docker lab-update
sudo install -d -m 0700 -o lab-update -g docker /opt/hiaf-lab-system/.hermes/update-requests
sudo install -d -m 0755 -o lab-update -g docker /opt/hiaf-lab-system/.hermes/updates
```

- server 运行 UID 需能写 `update-requests`（compose 里 `UPDATE_RUN_UID`）：

```bash
sudo setfacl -m u:${UPDATE_RUN_UID}:rwx /opt/hiaf-lab-system/.hermes/update-requests
```

- 生成 HMAC key（可选，但生产建议）：

```bash
sudo install -d -m 0755 /etc/lab-update
sudo sh -c 'head -c 32 /dev/urandom | base64 > /etc/lab-update/request.key'
sudo chmod 600 /etc/lab-update/request.key
sudo chown root:root /etc/lab-update/request.key
```

## 2. 安装

```bash
# 构建并安装 runner（二选一：宿主机 Go，或从镜像提取）
cd /opt/hiaf-lab-system/go-server
sudo go build -o /usr/local/bin/lab-update ./cmd/update-runner

sudo install -m 0755 /opt/hiaf-lab-system/deploy/systemd/lab-update-dispatch.sh /usr/local/bin/lab-update-dispatch
sudo install -m 0644 /opt/hiaf-lab-system/deploy/systemd/lab-update-dispatch.service /etc/systemd/system/
sudo install -m 0644 /opt/hiaf-lab-system/deploy/systemd/lab-update.path /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now lab-update.path
```

`lab-update.path` 监听 `update-requests/*.json`，触发 `lab-update-dispatch.service`；
dispatcher 逐个执行 `lab-update --request`，完成后把请求移到 `processed/`。

## 3. 切换 server

```bash
cd /opt/hiaf-lab-system/deploy
# 备份当前 compose 状态
docker compose -f docker-compose.yml ps > /tmp/lab-compose-before.txt
# 启用 host 模式（去掉 docker.sock）
docker compose -f docker-compose.yml -f docker-compose.host-update.yml up -d server
```

验证：

```bash
docker inspect lab-server --format '{{json .Mounts}}' | grep -q docker.sock && echo 'still mounted' || echo 'no socket'
curl -fsS http://127.0.0.1:8000/health
```

Web 触发一次 `--dry-run` 更新，确认：

- `journalctl -u lab-update-dispatch -f` 有请求日志；
- `.hermes/updates/lab-update-<session>.log` 持续写入；
- SSE 面板能看到日志并最终 done。

## 4. 回滚

```bash
cd /opt/hiaf-lab-system/deploy
docker compose -f docker-compose.yml up -d server
sudo systemctl disable --now lab-update.path
```

## 5. 注意

- dispatcher 串行执行；`lab-update` 内部还有仓库锁，手工更新与 Web 更新不会并发。
- `.cancel` 只在步骤边界生效，不会强杀正在回滚的进程。
- `request.key` 为空时不做 HMAC 校验；生产应配置并保持 server/runner 一致。
- 若 server 与宿主路径不同，需同时修改 systemd `Environment=` 与 compose 挂载。
