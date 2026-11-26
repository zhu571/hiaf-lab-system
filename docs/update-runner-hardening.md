# 更新执行器加固（AR-06b）设计

> 状态：设计稿，未实现。目标是把 `lab-server` 从「持有 docker.sock 的 root 等价权限」降为
> 「只能提交更新请求」，由宿主侧受控执行器完成 git pull + compose 重建。

## 1. 现状

`lab-server` 容器挂载 `/var/run/docker.sock`，`system` 模块通过 `docker run` 派发
`lab-update` runner 容器；runner 再以可写方式挂载仓库并执行 compose 重建。

问题：

- 能访问 docker.sock ≈ 宿主机 root；server 一旦被拿下，可创建特权容器、读宿主文件。
- server 容器同时挂载仓库、uploads、ntfy 配置、更新日志，攻击面大。
- runner 镜像/命令参数由 server 拼装，缺少第二道校验。

## 2. 目标

- server **不再挂载 docker.sock**。
- server 只能写「更新请求」；请求由宿主侧 systemd 单元执行。
- 保留现有 Web 触发、SSE 日志、失败回滚、并发互斥、超时。
- 默认行为可回滚到当前 `UPDATE_ENGINE=go`。

## 3. 推荐方案：宿主侧 systemd path 单元

### 3.1 数据流

```text
Web/API -> system.TriggerUpdate
        -> 写 .hermes/update-requests/<session>.json（0600，临时文件 + rename）
        -> systemd path unit 检测到新文件
        -> lab-update@<session>.service 以 lab-update 用户执行
             /usr/local/bin/lab-update --request <json>
        -> 写 .hermes/updates/<session>.log / .done
        -> server 轮询 log/done，继续 SSE 推送
```

### 3.2 请求文件

```json
{
  "session_id": "upd_20261126_120000_ab12",
  "requested_by": "user_id",
  "requested_at": "2026-11-26T12:00:00+08:00",
  "target_ref": "main",
  "nonce": "random",
  "hmac": "hex"
}
```

- `hmac = HMAC-SHA256(UPDATE_REQUEST_KEY, canonical_json_without_hmac)`。
- key 放 `/etc/lab-update/request.key`（0600，root），server 只读挂载；runner 校验后再执行。
- 请求文件由 server 写，目录 `0700`，仅 server UID 与 lab-update 组可读写。

### 3.3 systemd 单元

`/etc/systemd/system/lab-update@.service`：

```ini
[Unit]
Description=HIAF lab update runner (%i)
After=network-online.target docker.service
Requires=docker.service

[Service]
Type=oneshot
User=lab-update
Group=docker
WorkingDirectory=/opt/hiaf-lab-system
Environment=UPDATE_REQUEST_KEY_FILE=/etc/lab-update/request.key
ExecStart=/usr/local/bin/lab-update --request /opt/hiaf-lab-system/.hermes/update-requests/%i.json
TimeoutStartSec=1800
Nice=5

[Install]
WantedBy=multi-user.target
```

`/etc/systemd/system/lab-update.path`：

```ini
[Unit]
Description=Watch HIAF lab update requests

[Path]
PathExistsGlob=/opt/hiaf-lab-system/.hermes/update-requests/*.json
Unit=lab-update@.service

[Install]
WantedBy=paths.target
```

实际实现建议用 `systemd.path` + 一个小的 dispatcher（把文件名映射到 `%i`），
或 `path` 触发固定 service，由 service 脚本扫描目录并逐个执行；避免模板实例名包含路径。

### 3.4 server 侧

- 新增 `UPDATE_ENGINE=host`，`sessionRunner` 选择 `hostRunner`：
  - `Spawn`：校验参数 → 写请求文件 → 返回 session id；
  - `Wait`/`Stream`：轮询 `.hermes/updates/<id>.log` 与 `.done`；
  - `Kill`：写 `.cancel` 标记，由 runner 在步骤边界检查。
- `compose` 删除 `server.volumes` 中的 `/var/run/docker.sock` 与 `group_add`。
- 新增挂载：
  - `${REPO_ROOT}/.hermes/update-requests:...:rw`
  - `${REPO_ROOT}/.hermes/updates:...:rw`（已有）
  - `UPDATE_REQUEST_KEY_FILE:/etc/lab-update/request.key:ro`
- server 不再需要 `UPDATE_DOCKER_GID`。

### 3.5 runner 侧

- 复用现有 `cmd/update-runner` 的 7 步流水线；新增 `--request` 模式：
  - 读取/验签请求；
  - 获取 `flock`（与手工更新互斥）；
  - 执行 `git pull` + `docker compose build/up`；
  - 写 log/done；失败写 `.failed` 与原因。
- runner 二进制以 `lab-update` 用户运行，属于 `docker` 组；仍具 root 等价能力，但
  **只有宿主上的受控进程**能触发，server 无法直接调用 Docker API。

## 4. 备选方案

### A. docker-socket-proxy

- 新增 `tecnativa/docker-socket-proxy`，只暴露 `CONTAINERS/IMAGES/NETWORKS/VOLUMES` 与 `POST`。
- server 设 `DOCKER_HOST=tcp://proxy:2375`，去掉 socket 挂载。
- 问题：`docker compose run` 需要创建/启动容器、挂载卷，代理仍需较宽权限；
  server 被拿下后仍可创建特权容器（除非代理只允许固定容器名，而这会破坏更新 runner）。
- 结论：可作为过渡，但不满足 AR-06b 的强隔离目标。

### B. 宿主机 cron 轮询请求目录

- 不需要 systemd path；cron 每分钟扫描。
- 延迟高、并发控制弱，仅作降级。

## 5. 实施步骤

1. 增加 `docs/update-runner-hardening.md`（本文件）与 systemd 单元示例。
2. `hostRunner` + `UPDATE_ENGINE=host`，默认仍 `go`；补单元测试（请求写盘、验签、日志轮询）。
3. `cmd/update-runner --request` 模式与验签；复用现有流水线测试。
4. compose 增加 host 模式 override / `.env.example`；默认不切换。
5. 生产切换：安装 systemd 单元 → 设置 `UPDATE_ENGINE=host` → 删除 server socket 挂载 →
   做一次演练更新 → 回滚演练。
6. 观察一周后删除 `dockerRunner` 与 socket 挂载。

## 6. 验证

- `UPDATE_ENGINE=host` 下 Web 触发更新，SSE 能持续输出，最终 `.done`。
- 篡改请求 HMAC → runner 拒绝，server 收到 failed。
- 并发触发 → 第二个请求被互斥拒绝或排队。
- 删除 server 的 docker.sock 后，server 其他功能（上传、DB、AI）不受影响。
- `docker inspect lab-server` 不再包含 `/var/run/docker.sock`。

## 7. 回滚

- `.env` 改回 `UPDATE_ENGINE=go`，恢复 server socket 挂载与 `group_add`，重启 server。
- 宿主侧 systemd 单元可保留，不影响旧链路。
