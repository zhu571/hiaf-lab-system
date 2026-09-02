# labctl — hiaf-lab-system 命令行客户端 + MCP Server

AI Agent（Hermes/Codex/Goose）或运维人员通过 CLI / MCP 工具直接操作 hiaf-lab-system，
不手拼 HTTP 请求。纯 HTTP 客户端封装，不碰服务端逻辑；服务端权限/审计/限流不变。

- 设计：`.hermes/plans/2026-08-11_optimization-queue.md` §J（v5）；全量补全方案见
  `.hermes/reports/labctl-gap-analysis-zcode.md`（差距分析）与
  `.hermes/reports/labctl-implement-summary.md`（实施总结）
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
labctl logs create prj_001 --content "RF 匹配完成" --category rf
labctl daily-report submit rep_1 # 提交日报（闭环最后一步）
labctl attachments upload 曲线.png --entity-type log --entity-id log_1
```

输出默认 JSON（Agent 易解析）；`--human` 输出人类可读格式。错误统一输出到 stderr：

```json
{ "error": { "code": "permission_denied", "message": "当前用户无权访问该项目", "request_id": "req_xxx" } }
```

`request_id` 为服务端透传，可用于追审计日志（`labctl audit get <request_id>`）。
退出码：`0` 成功、`1` 一般错误（含日报提交 blocked）、`2` 认证类错误（401 / 未登录 / 已过期）。

`--base-url` 优先级：显式参数 > 已登录 token 文件中的服务器地址 > `LABCTL_BASE_URL` 环境变量 > `http://127.0.0.1:8000`（未传参且未登录时）。

## 认证双通道

| 通道 | 方式 | 凭证 | 说明 |
|------|------|------|------|
| 交互 | `labctl login <用户名>` | 用户名/密码 | access/refresh/csrf 三 token 存 `~/.labctl/token`（**0600**，目录 0700；密码不落盘）；access 过期自动 refresh 续期 |
| 无人值守 | `LABCTL_SERVICE_TOKEN` 环境变量 | 服务账号 JWT | CLI 纯透传（不扩展服务端 SERVICE_TOKEN 白名单）；服务端校验/审计不变。401 后不自动 refresh，需更新 token 重试 |

CI/Agent 场景免交互登录：`echo -e "username\npassword" | labctl login --token-stdin`。

自助操作（交互通道）：`labctl login password`（改密，交互确认）、`labctl login set-language zh|en`。

> **服务账号通道写操作注意**：服务端 CSRF 中间件要求写请求带 `X-CSRF-Token`（与 cookie 匹配），
> 该 token 只能由登录响应获得——服务账号 JWT 透传通道没有 csrf token，**写操作会返回
> 403 `csrf_failed`（原样透传 + request_id）**，只读操作为主；需要写操作请用交互登录通道。

## 子命令覆盖表（23 子命令 / 103 动作）

| 子命令 | 动作 | 端点（main.go 已核实） | 角色要求（服务端强校验） |
|--------|------|------------------------|--------------------------|
| `login` | 登录 / `--logout` / `--whoami` / `password` / `set-language` | POST /auth/login、/auth/logout、GET /auth/me、POST /auth/change-password、PATCH /auth/profile | 任意启用用户 |
| `daily-report` | `today` / `history` / `entry` / `submit` / `ai-parse` / `by-date` | POST /daily-reports/today、GET /daily-reports、GET+PATCH /daily-reports/{id}、POST /{id}/submit、POST /{id}/ai-parse、GET /by-date | 日报仅本人可见/可改；submit blocked=true 时退出码 1（加 --force 越过） |
| `logs` | `list` / `get` / `create` / `update` | GET/POST /projects/{id}/logs、GET/PATCH /logs/{id} | list/get viewer+；create/update member+（update 自己的，maintainer+ 改任意，仅 draft） |
| `my-logs` | （顶层命令，同 logs 筛选参数 + `--all`） | GET /logs/mine | 本人跨项目日志（含仍可读的 completed/archived 项目） |
| `projects` | `list` / `get` / `create` / `update` / `transition` / `members list·add·set-role·remove` | GET/POST/PATCH /projects、POST /{id}/transition、GET/POST/PATCH/DELETE /{id}/members… | create 限 maintainer/admin；transition 限 owner/admin（有警告须 `--ignore-warnings`）；members 需 manage_members |
| `issues` | `list` / `get` / `create` / `update` / `transition` / `comment` | GET/POST /projects/{id}/issues、GET/PATCH /issues/{id}、POST /{id}/transition、POST /{id}/comments | list/get viewer+；create/update member+；comment 按项目 comment_policy |
| `test-data` | `list` / `entry` / `batch` / `get` / `update` / `invalidate` | GET/POST /projects/{id}/test-data(+batch)、GET/PATCH/DELETE /test-data/{id} | list/get viewer+；写 member+；invalidate 记录者/owner |
| `runs` | `list` / `get` / `create` / `delete` / `status` / `steps list·add·status·reorder` / `report-link` / `report-unlink` | GET/POST /projects/{id}/experiment-runs、GET/PATCH/DELETE /experiment-runs/{id}、steps 与 run-steps 系列端点 | list/get viewer+；create/steps 写 member+；status/delete/reorder/关联日报 maintainer+ 或创建者 |
| `alerts` | `list` / `get` / `resolve` | GET /alerts、GET /alerts/{id}、POST /alerts/resolve | 列表/详情全员；resolve 限 admin/maintainer |
| `experiences` | `extract-candidates` / `list` / `get` / `create` / `update` / `publish` / `archive` | POST /experiences/extract-candidates、GET/POST/PATCH /experiences、POST /{id}/publish、/{id}/archive | extract 限 maintainer/admin；create 项目 member+（全局仅 admin）；publish 项目 maintainer+；archive 项目 owner |
| `weekly` | `generate` / `recent` | POST /weekly/summary、GET /experiences?tags=weekly_summary | generate 限 maintainer/admin |
| `attachments` | `upload` / `list` / `download` / `link` / `unlink` / `rm` | POST /attachments（multipart）、GET /attachments、GET /{id}/content、POST /{id}/links、DELETE /{id}/links/{lid}、DELETE /{id} | 上传未绑定附件任意登录用户；绑定/读写需目标实体权限；下载带 sha256 校验 |
| `todos` | `list` / `add` / `edit` / `done` / `defer` / `rm` | GET/POST/PATCH/DELETE /todos 系列 | 登录即可；edit 仅 owner（updated_at 乐观锁，缺省自动从列表取） |
| `audit` | `events` / `verify` / `get` | GET /audit/events、/audit/verify、/audit/{request_id} | 仅 admin/maintainer |
| `sensors` | `latest` / `history` | GET /sensors/latest、/sensors/history | 登录（from/to/interval 是 Flux 字面量如 `-1h`/`now()`/`5m`） |
| `ask` | `chat` / `history` | POST /ask/chat、GET /ask/history | 登录 + 限流 10 次/min |
| `assembly` | `list` / `transition` | GET /projects/{id}/assembly、PATCH /assembly/{id} | list viewer+；transition member+（越依赖需 --override-reason） |
| `step-templates` | `list` / `generate` | GET /step-templates、POST /step-templates/generate | list viewer+；generate 项目 member+（AI 生成，限流 10 次/min） |
| `rf-matching` | `list` / `create` | GET/POST /projects/{id}/rf-matching | list viewer+；create member+ |
| `admin` | `users list·create·set·reset-password` / `invites list·create·revoke` | /admin/users 系列、/admin/invitation-codes 系列 | 全部仅 admin |
| `automation` | `rules list·create·enable·disable·rm` | /admin/automation/rules 系列 | 全部仅 admin |
| `agent` | `candidates list·trace·approve·reject` | GET /agent/candidates、/{id}/trace、/{id}/approve、/{id}/reject | 仅 admin/maintainer（Agent 审核队列补充入口） |
| `instruments` | `list` / `status` / `whitelist` / `parse-result` / `emergency-stop INSTRUMENT_ID [--yes]` | GET /instruments、/{id}/status、/whitelist、POST /{id}/parse-result、POST /{id}/emergency-stop | 前 4 个 viewer+（只读）；emergency-stop 登录即可（唯一写命令例外：TTY 一次确认、无 TTY 须 `--yes`，急停后需 maintainer/admin 人工复核） |
| `update` | `status` / `run [--no-wait] [--timeout N]` | GET /admin/system/version、POST /admin/system/update、GET /update/stream/{id}（SSE） | 全部限 admin |

**刻意不做的**（后端有端点但不进 CLI）：issues delete（后端无此端点）、alerts report /
ask execute / agent tasks（SERVICE_TOKEN 或 agent 角色专用内部通道）、translations
（低频且权限模型复杂）、instruments **常规写命令**（commands/leases/approvals/gascell/piezo——
仪器安全链路租约+审批+白名单分级在 Web 有完整 UX 支撑，CLI 略过审批链容易误操作）。
唯一例外 emergency-stop：失败方向安全（只下发白名单安全停序列并锁定仪器），带 TTY
确认且不注册 MCP 工具；其余写入口维持排除。

## 常用链路示例

```bash
# 日志录入闭环：拿今日日报 → 写日报 → 录日志（关联日报）→ 确认 → 提交
labctl daily-report today
labctl daily-report entry rep_1 --raw-text "今日降温到 4.2K，RF 匹配 pass"
labctl logs create prj_001 --content "降温到 4.2K" --category cryo --daily-report-id rep_1
labctl logs update log_1 --confirm
labctl daily-report submit rep_1            # 有警告时 blocked + 退出码 1，确认后 --force

# AI 解析日报（自动拆日志草稿）
labctl daily-report ai-parse rep_1

# 批量测试数据（JSON 或表头 CSV，任一行失败整批 422 行级错误透出）
labctl test-data batch prj_001 --file data.csv
labctl test-data batch prj_001 --json '[{"data_type":"cryo","measurement":"t","value":4.2}]'

# 附件：上传直挂日志 → 列表 → 下载（sha256 校验）→ 补挂/解绑/删除
labctl attachments upload 曲线.png --entity-type log --entity-id log_1 --description "匹配曲线"
labctl attachments list --entity-type log --entity-id log_1
labctl attachments download att_1 --output /tmp/曲线.png
labctl attachments link att_1 --entity-type issue --entity-id iss_1
```

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
- 写操作自动带 `Idempotency-Key`（uuid4）+ `X-CSRF-Token`（登录/refresh 响应下发，跨进程从 token 文件恢复）；409 幂等键重复错误原样透传（附件上传命中时附「先核实勿盲目重试」提示——服务端幂等键先插库再执行 handler，5xx 后同 key 重试会撞唯一索引，属防重放语义）；
- `attachments download` 是 GET 但服务端 handler 级要求 `Idempotency-Key` 头，由 `api_client.download()` 专项补上（流式写盘 + 本地 sha256 与服务端比对）；
- `logout` 会把 refresh token 以 Cookie（Path=/api）回填后再调 `POST /auth/logout`，**服务端真正撤销 refresh token**（服务端 Logout 只读 Cookie、忽略请求体），随后清除本地凭证；
- 401 → 自动 refresh 一次；refresh 失败 → 提示重新 `labctl login`（退出码 2）；
- 429 / 5xx → 指数退避重试（2^attempt，共 3 次），耗尽后按场景提示（429 限流 / 502 上游降级）；multipart 上传先整体读入内存（≤100 MiB 本地预检同阈值）保证重试可重放；
- 403 / 404 / 422 等错误原样透传，含 request_id；`LabctlError.details` 透传服务端 `details`（如 test-data batch 的行级 `errors[]`）；
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

工具前缀 `labctl_*`（**103 个**，与 CLI 动作一一对应；`labctl_update_trigger` 只触发
不跟踪——SSE 日志流不适合 MCP 轮询模型，由 Web 端 SSE 或 `labctl update run` 订阅）。
`labctl_attachments_upload/download` 走 MCP 主机本地文件路径（MCP 仅限内网受信主机启动）。
账号自助（改密/切语言）未注册为 MCP 工具（会话进程绑定，改密应走 CLI）。
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

全部用 mock HTTP（httpx.MockTransport），不依赖真实服务端；multipart/download/CSV 解析
均有专项用例（boundary 字段、Idempotency-Key 头、sha256 落盘比对、行级 422 透传）。
