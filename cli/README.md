# labctl — hiaf-lab-system 命令行客户端 + MCP Server

AI Agent（Hermes/Codex/Goose）或运维人员通过 CLI / MCP 工具直接操作 hiaf-lab-system，
不手拼 HTTP 请求。纯 HTTP 客户端封装，不碰服务端逻辑；服务端权限/审计/限流不变。

- 设计：`.hermes/plans/2026-08-11_optimization-queue.md` §J（v5）
- 复用：`py-agent/tools/api.py` GoAPI 的 REST 客户端模式（base URL / token 注入 / 错误处理）
- 依赖：`click` / `httpx` / `mcp`（版本与 py-agent 同一套 pin，见 `requirements.txt`）

## 安装与运行

```bash
# 复用 py-agent 虚拟环境（依赖已 pin 且一致）
py-agent/.venv/bin/pip install -r cli/requirements.txt
export PATH="$PWD/py-agent/.venv/bin:$PATH"

labctl --help
labctl login zhangsan            # 交互登录（提示输入密码）
labctl daily-report today        # 获取/创建今日日报
labctl projects list --status active
labctl alerts list --status active
```

输出默认 JSON（Agent 易解析）；`--human` 输出人类可读格式。错误统一输出到 stderr：

```json
{ "error": { "code": "permission_denied", "message": "当前用户无权访问该项目", "request_id": "req_xxx" } }
```

`request_id` 为服务端透传，可用于追审计日志。退出码：`0` 成功、`1` 一般错误、`2` 认证类错误（401 / 未登录 / 已过期）。

`--base-url` 优先级：显式参数 > 已登录 token 文件中的服务器地址 > `LABCTL_BASE_URL` 环境变量 > `http://127.0.0.1:8000`（未传参且未登录时）。

## 认证双通道

| 通道 | 方式 | 凭证 | 说明 |
|------|------|------|------|
| 交互 | `labctl login <用户名>` | 用户名/密码 | access/refresh/csrf 三 token 存 `~/.labctl/token`（**0600**，目录 0700；密码不落盘）；access 过期自动 refresh 续期 |
| 无人值守 | `LABCTL_SERVICE_TOKEN` 环境变量 | 服务账号 JWT | CLI 纯透传（不扩展服务端 SERVICE_TOKEN 白名单）；服务端校验/审计不变。401 后不自动 refresh，需更新 token 重试 |

CI/Agent 场景免交互登录：`echo -e "username\npassword" | labctl login --token-stdin`。

> **服务账号通道写操作注意**：服务端 CSRF 中间件要求写请求带 `X-CSRF-Token`（与 cookie 匹配），
> 该 token 只能由登录响应获得——服务账号 JWT 透传通道没有 csrf token，**写操作会返回
> 403 `csrf_failed`（原样透传 + request_id）**，只读操作为主；需要写操作请用交互登录通道。

## 子命令覆盖表（11 子命令 / 29 动作）

| 子命令 | 动作 | 端点（main.go 已核实） | 角色要求（服务端强校验） |
|--------|------|------------------------|--------------------------|
| `login` | 登录 / `--logout` / `--whoami` | POST /auth/login、/auth/logout、GET /auth/me | 任意启用用户 |
| `daily-report` | `today` / `history` / `entry <id> [--raw-text]` | POST /daily-reports/today、GET /daily-reports、GET+PATCH /daily-reports/{id} | 日报仅本人可见/可改 |
| `projects` | `list` / `get <id>` / `create` | GET /projects、GET /projects/{id}、POST /projects | create 限 maintainer/admin |
| `issues` | `list <pid>` / `create <pid>` / `transition <id>` | GET/POST /projects/{id}/issues、POST /issues/{id}/transition | 列表读：viewer+；创建：member+；流转：服务端按状态机校验 |
| `test-data` | `list <pid>` / `entry <pid>` | GET/POST /projects/{id}/test-data | 列表 viewer+；录入 member+ |
| `runs` | `list <pid>` / `get <id>` / `status <id> --action` | GET /projects/{id}/experiment-runs、GET /experiment-runs/{id}、PATCH /experiment-runs/{id} | 列表 viewer+；status 流转由服务端状态机校验 |
| `alerts` | `list` / `resolve <id>` | GET /alerts、POST /alerts/resolve | 列表全员；resolve 限 admin/maintainer |
| `logs` | `list <pid>` / `get <id>` | GET /projects/{id}/logs、GET /logs/{id} | 列表 viewer+；详情 service 内校验 |
| `experiences` | `extract-candidates` / `list` / `publish <id>` | POST /experiences/extract-candidates、GET /experiences、POST /experiences/{id}/publish | extract 限 maintainer/admin；publish 项目 maintainer+（全局仅 admin） |
| `weekly` | `generate` / `recent` | POST /weekly/summary、GET /experiences?tags=weekly_summary | generate 限 maintainer/admin |
| `update` | `status` / `run [--no-wait] [--timeout N]` | GET /admin/system/version、POST /admin/system/update、GET /admin/system/update/stream/{id}（SSE） | 全部限 admin |

## 系统更新（admin）

```bash
labctl update status                     # 版本差异：current/latest/behind/can_update
labctl update run                        # 触发更新 + 实时跟踪 SSE 日志流直到结束
labctl update run --no-wait              # 只触发不跟踪（脚本后台场景），打印 session_id
labctl update run --timeout 3600         # 日志流读超时（秒），默认 2400（服务端 30min 看门狗略留余量）
```

- **需 admin 交互登录**：`labctl login <admin 用户名>`。`LABCTL_SERVICE_TOKEN`
  服务账号通道没有 csrf token，触发更新（写操作）会被服务端 CSRF 校验拒绝
  （403 `csrf_failed`，CLI 附带"需要 admin 登录"提示）；`update status` 为只读，不受影响。
- `run` 默认订阅 `GET /admin/system/update/stream/{sessionId}`，把每个事件实时打印：
  JSON 模式逐事件输出 `{"event": "...", "data": {...}}` 行；`--human` 渲染成
  `[UPDATE] 步骤 3/7：git pull` 样式可读行。
- 退出码：`done` 且成功 = 0；收到 `error` 事件、`done` 但 `exit_code` 非零 /
  `success=false`、或日志流异常中断（超时/断连/未收到结果事件）= 1。
- 常见服务端错误透传：409 `update_in_progress`（已有更新在执行）、
  500 `script_missing` / `update_trigger_failed`。
- 更新期间 server 可能重建（compose up），日志流断连属预期——runner 侧有磁盘日志，
  更新结束后重连同一 session 可回放完整日志；session 内存保留 1 小时。

## 安全设计

- token 文件 `~/.labctl/token` 0600、目录 0700，密码不落盘；
- 写操作自动带 `Idempotency-Key`（uuid4）+ `X-CSRF-Token`（登录/refresh 响应下发，跨进程从 token 文件恢复）；409 幂等键重复错误原样透传；
- `logout` 会把 refresh token 以 Cookie（Path=/api）回填后再调 `POST /auth/logout`，**服务端真正撤销 refresh token**（服务端 Logout 只读 Cookie、忽略请求体），随后清除本地凭证；
- 401 → 自动 refresh 一次；refresh 失败 → 提示重新 `labctl login`（退出码 2）；
- 429 / 5xx → 指数退避重试（2^attempt，共 3 次），耗尽后按场景提示（429 限流 / 502 上游降级）；
- 403 / 404 等错误原样透传，含 request_id；
- CLI 不做本地角色判断——权限全部由服务端强校验（403 原样透传）；
- 写操作仅限内网直连执行（公网来源被服务端 source gate 白名单拦截，属服务端行为，CLI 原样透传）；
- agent 角色 token 不可用于 CLI（服务端 AgentContext 校验），以被授权用户 token 运行即可。

## MCP Server

```bash
# 方式一：先登录（凭证仅存于 MCP 进程内存，不落盘）
py-agent/.venv/bin/python -m cli.mcp_server

# 方式二：启动即带服务账号 token（无人值守）
LABCTL_SERVICE_TOKEN=<jwt> py-agent/.venv/bin/python -m cli.mcp_server
```

工具前缀 `labctl_*`（23 个，含 `labctl_update_status` / `labctl_update_trigger`——SSE 日志流
不适合 MCP 轮询模型，未暴露为工具），全部复用 `cli/commands.py` 命令执行函数，全部经 REST 调用服务端。
`labctl_logout` 后进程内会话清空（重新调用工具前需再次 `labctl_login` 或设 `LABCTL_SERVICE_TOKEN`）。
MCP 自身无独立鉴权——**安全边界 = 启动进程所持 token 的权限**：仅限内网/受信主机启动，
不在公网暴露 MCP stdio 桥；工具调用由服务端权限/审计/限流兜底。

Claude Desktop / Cursor / Goose 配置示例：

```json
{
  "mcpServers": {
    "labctl": {
      "command": "/opt/hiaf-lab-system/py-agent/.venv/bin/python",
      "args": ["-m", "cli.mcp_server"],
      "cwd": "/opt/hiaf-lab-system",
      "env": { "LABCTL_SERVICE_TOKEN": "<service-account-jwt>" }
    }
  }
}
```

## 测试

```bash
# 必须在仓库根目录运行（cli/ 目录下有 cli.py 文件，避免遮蔽 cli 包）
py-agent/.venv/bin/python -m unittest discover -s cli/tests -v
```

全部用 mock HTTP（httpx.MockTransport），不依赖真实服务端。
