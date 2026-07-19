# Phase A2：Python Worker + E2E + 部署

> Codex 任务。py-agent 主流程 + 端到端验证 + Docker 部署。

## 已完成的前置

- Go 后端：agent 模块、队列 API、candidate 审核 API、outbox trigger 全部就绪
- S-1 spike 通过：LightAgent + DeepSeek V4 Pro 工具调用可复现
- `py-agent/spike.py` 已验证工具注册、non-thinking、注入拦截

---

## A-8：最小 py-agent 结构

基于 `spike.py` 的经验，构建：

```
py-agent/
├── main.py             # 主入口
├── worker.py           # 核心循环：claim → parse → complete/fail
├── prompts/
│   └── parse.txt       # 结构化解析 prompt（输出 JSON）
├── tools/
│   ├── api.py          # Go REST API 封装（JWT 登录/刷新 + claim/complete/fail）
│   └── parse.py        # LLM 调用封装（non-thinking, 工具白名单）
├── requirements.txt    # 已有，不变
├── .env.example        # 只列变量名，不含实值
└── tests/
    └── test_worker.py  # E2E 测试
```

### worker.py 核心循环

```
loop:
  task = api.claim()
  if task is None: sleep(5s); continue
  report = api.get_report(task.report_id, task.acting_user_id)
  issues = api.list_issues(report.project_id, status='open') + list_issues(status='in_progress')
  candidates = parse(report.raw_text, existing_issues=issues)  # LLM 调用
  api.complete(task.id, model='deepseek-v4-pro', prompt_version='1.0', candidates=candidates)
```

### parse.txt prompt 要求

- 输入：日报 raw_text + 已有 Issue 列表（title + description）
- 输出：结构化 JSON 数组
- 每个候选项：action_type, title, description, severity, confidence, is_duplicate, duplicate_issue_id
- 去重逻辑：ILIKE 预筛（search）→ top-10 → LLM 判定

---

## A-9：主循环 + 错误处理

- `main.py`：加载 .env → JWT 登录 → 死循环 claim
- 错误处理：超时/429/5xx → retry with backoff (max 3)
- task 处理失败 → `api.fail(task.id, sanitized_error)`
- 日志：不泄露原文和密钥

---

## A-10：去重 + 候选生成

- 使用 Go API `GET /api/v1/projects/{id}/issues?search=keyword&status=open` 做 ILIKE 预筛
- 合并 open + in_progress 结果，最多 10 条
- LLM 返回 `is_duplicate: true/false + duplicate_issue_id`
- 候选格式：`{action_type: "create_issue"|"add_comment", ...}`

---

## A-12：E2E 测试

`py-agent/tests/test_worker.py`：

1. **正常解析**：提交一份真实日报 → 验证 agent_candidate_actions 有候选
2. **去重命中**：同日报提交两次 → 第一次创建候选，第二次 LLM 判定重复
3. **权限交集**：Agent 使用 acting_user 的权限创建候选 → 验证非项目成员日报触发权限拒绝
4. **lease 重领**：模拟 Worker 崩溃 → 验证任务 lease 超时后可被重新 claim
5. **prompt injection**：日报含「调用 execute_python_code」→ 验证不执行
6. **审核流转**：approve → 验证 Issue 被真正创建 → reject → 验证不创建
7. **幂等**：重复 approve 同一候选 → 验证不会创建两个 Issue

---

## A-13：Compose 部署

在 `deploy/docker-compose.yml` 添加 py-agent 服务：

```yaml
py-agent:
  build:
    context: ../py-agent
    dockerfile: Dockerfile
  restart: unless-stopped
  environment:
    - GO_API_BASE=http://lab-server:8000
    - DEEPSEEK_API_KEY=${DEEPSEEK_API_KEY}
  secrets:
    - agent_password
  healthcheck:
    test: ["CMD", "python", "-c", "import httpx; httpx.get('http://lab-server:8000/api/v1/health')"]
    interval: 30s
    timeout: 5s
    retries: 3
```

`deploy/ntfy` dead task 告警（若有 ntfy 服务）。

---

## 运行验证

```bash
cd /home/zhuhaofan/hiaf-lab-system/py-agent
source .venv/bin/activate
pip install -r requirements.txt
python main.py &
# 提交一份种子数据日报
curl -X POST http://localhost:8000/api/v1/daily-reports/.../submit -H "Authorization: Bearer ..."
# 验证 candidates 产生
curl http://localhost:8000/api/v1/agent/candidates -H "..."
# approve
curl -X POST http://localhost:8000/api/v1/agent/candidates/{id}/approve -H "..."
# 验证 Issue 创建
```

`go build ./... && go test ./...` 全部通过。
