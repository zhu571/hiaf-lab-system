设计两个功能的方案，写到 .hermes/plans/experiment-testdata-plan.md

## 背景

用户需要实现两个功能：

### 1. 实验步骤 AI 助手
现状：
- 装配步骤已有完整的 AI 生成 + 模板库（AssemblyView + StepTemplatesView），迁移 024 + steptemplates 模块 + py-agent stepplan
- 实验流程（RunDetailView）没有步骤拆分，没有 AI 辅助

需要设计：
- run_steps 表（迁移 025）
- RunDetailView 中加 AI 生成步骤对话框（可复用 StepItemsEditor.vue）
- 步骤模板支持 experiment kind（已有但 MVP 只存了 assembly）
- 应用到实验流程的 API

### 2. 仪器测量保存测试数据
现状：
- InstrumentMeasureView 执行命令后只显示 cmdResult，没有存储功能
- 后端已有完整的 testdata 模块（CRUD API, 模型含 SourceInstrument）
- 前端已有 TestDataView.vue（录入/查询/编辑）

需要设计：
- 执行结果旁加「保存到测试数据」按钮
- 弹出 dialog 填写 data_type/measurement/value/unit
- 调用 POST /api/v1/test-data

## 阅读这些文件了解边界
- go-server/runs/model.go
- go-server/runs/repository.go  
- go-server/testdata/model.go
- go-server/testdata/handler.go
- web-ui/src/views/RunDetailView.vue
- web-ui/src/views/InstrumentMeasureView.vue
- web-ui/src/views/TestDataView.vue
- .hermes/plans/step-templates-plan.md
- migrations/024_step_templates.up.sql

## 输出
方案写到 .hermes/plans/experiment-testdata-plan.md，包含：
- 数据模型变更
- API 设计（新增和复用的端点）
- 前端改动点
- 复用哪些现有组件/代码
- 预估工作量
