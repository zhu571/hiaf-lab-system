重构两个页面布局。

## 需求

### 1. 实验批次详情页（RunDetailView.vue）
AI 生成步骤按钮从「步骤」tab 内部移到**页面顶栏**（像 AssemblyView 一样常驻显示）。
- 页面顶部现有 `v-if="canEdit" class="actions"` 区域（line 16）——把「AI 生成步骤」「从模板导入」「手动新建」三个按钮放进去
- 从「步骤」tab 内部的 steps-toolbar 移除这三个按钮（或保留一个精简版）
- 用户不点「步骤」tab 也能看到 AI 生成入口

### 2. 仪器测量页（InstrumentMeasureView.vue）布局重构
把 AI 对话从抽屉（el-drawer）改为**页面常驻区域**，曲线窗口也常驻显示：

a) **AI 对话常驻**：
- 删除 el-drawer（line 124-186）
- 在页面下方新增一个常驻「AI 对话」面板（section.panel）：
  - 仪器选择下拉（复用现有 aiInstrument）
  - 消息列表（复用现有 AI 消息渲染）
  - 输入框 + 发送按钮
- 对话始终可见，不需要点击才弹侧栏

b) **命令白名单折叠**：
- 命令白名单不再直接显示（line 96-122 的 section）
- 改为页面顶栏一个按钮「命令白名单」→ 点击弹出 el-dialog 或展开/收起
- 默认收起

c) **曲线窗口常驻**：
- cmdResult 的曲线保持（执行命令后显示）
- 确保曲线图（sweep_xy）在常驻区域里展示，有足够的宽度

### 约束
- 保持现有功能、API 调用、i18n 逻辑不变
- 新增 UI 文案走 i18n（zh.ts/en.ts 补 key）
- 样式沿用现有 panel/panel-head 体系
- npm run build 验证

先读文件理解现状，再动手。
