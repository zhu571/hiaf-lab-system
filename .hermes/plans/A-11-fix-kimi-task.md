# A-11 小修：候选审核页日报链接直达

## 后端

agent_candidate_actions 表有 task_id，通过 task 可以拿到 report_id。

修改 `GET /api/v1/agent/candidates` 响应，每个候选增加 `report_id` 字段（通过 task 关联查询）。

## 前端

AgentCandidatesView.vue 的「来源日报」列改为直接链接到 `/daily-reports/{report_id}`。
