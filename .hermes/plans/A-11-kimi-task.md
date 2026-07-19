# A-11：前端 Agent 候选审核页

> Kimi Code 任务。新增 AgentCandidatesView.vue。

## 目标

在 `web-ui/src/views/` 下新建 `AgentCandidatesView.vue`，用于查看、批准、拒绝 AI 生成的候选动作。

## API

| 端点 | 用法 |
|------|------|
| `GET /api/v1/agent/candidates?status=pending_review&page=1&per_page=20` | 候选列表 |
| `POST /api/v1/agent/candidates/{id}/approve` | 批准 → 创业务记录 |
| `POST /api/v1/agent/candidates/{id}/reject` | 拒绝 → 保存理由 |

## 数据模型

```json
{
  "id": "uuid",
  "task_id": "uuid",
  "action_type": "create_issue / add_comment / create_experience",
  "project_id": "uuid",
  "payload": {
    "title": "...",
    "description": "...",
    "severity": "medium",
    "is_duplicate": false,
    "duplicate_issue_id": null
  },
  "status": "pending_review / approved / rejected / executed / execution_failed",
  "agent_confidence": 0.85,
  "reviewed_by": null,
  "reviewed_at": null,
  "review_reason": null,
  "created_at": "2026-07-19T..."
}
```

## 页面设计

### 候选列表
- 表格式展示：日期、类型标签（创建Issue/追加评论/创建经验）、标题、置信度、来源日报链接、操作按钮
- 状态筛选：待审核 / 已批准 / 已拒绝
- 分页

### 候选详情
- 展开/弹窗显示 payload 详情：标题、描述、严重程度
- 若是追加评论：显示目标 Issue 和评论内容
- 显示 LLM 置信度和 prompt 版本

### 审核操作
- 「批准」按钮 → 弹确认框 → 调 approve API
- 「拒绝」按钮 → 弹原因输入框 → 调 reject API
- 操作后刷新列表

### 小功能
- 添加路由 `web-ui/src/router/` 中 `/agent-candidates`
- API 文件 `web-ui/src/api/agent.ts`
- Issue 列表页已有的 `ai_generated` 标签列（审批后才出现）

## 风格

对齐项目现有 Vue 3 + Element Plus 风格。参考 IssuesView.vue 的表格/筛选/分页模式。
