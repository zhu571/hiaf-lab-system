继续排查批次界面（RunDetailView）为什么看不到 tab 和 AI 生成步骤按钮。

## 已知事实（用户反馈 + 已验证）
1. 用户是 admin 角色
2. 浏览器缓存已彻底清理（Firefox 站点存储 + HTTP 缓存 1.1GB 已删）
3. **装配界面（AssemblyView）能看到「AI 生成步骤」和「模板库」按钮** ← 关键！说明新版前端已经在跑，不是部署/缓存问题
4. 批次界面（实验批次详情，/experiment-runs/:id）看不到顶部 tab（应该有 概览/步骤/关联日报/测试数据 4 个 tab），也看不到「AI 生成步骤」按钮
5. 线上验证：http://10.144.144.12:8000/assets/RunDetailView-BTVjzuW7.js 包含 stepsTab、aiGenerateSteps 等 key
6. 线上验证：入口 index-BpjK9tvg.js 引用 RunDetailView-BTVjzuW7.js（新版）
7. 路由：/experiment-runs/:id → RunDetailView 懒加载

## 需要深入排查的方向

既然装配界面正常（新版在跑），批次界面异常，那么问题很可能在：

### A. RunDetailView 组件运行时错误（重点）
- RunDetailView.vue 模板/script 里是否有运行时错误导致组件渲染失败或 tab 不渲染？
- 检查是否有 `v-if="run"` 之类的条件，如果 run 数据没加载出来，整个详情（含 tab）都不显示
- 检查 load() 流程：getRun() 失败会怎样？是否显示错误页而不是 tab？
- 检查 tab 是否包在某个 v-if 里（比如 v-if="run" / v-if="canEdit" / v-if="!loading"）

### B. 浏览器控制台 JS 错误
- 用浏览器打开 http://10.144.144.12:8000/ 登录后进入批次详情，看 console 报什么错
- 常见：TypeError、组件未注册（el-tab-pane 需要 el-tabs 包裹）

### C. 后端 API 是否正常
- GET /api/v1/experiment-runs/{id} 是否返回数据？
- 如果 run 数据为空/404，前端可能显示"批次不存在"而不渲染 tab

### D. chunk 完整性
- 线上 RunDetailView-BTVjzuW7.js 是否完整？用 node 检查 JS 语法能否解析
- 是否只 grep 到 key 字符串但实际组件代码缺失（比如构建产物不完整）？

## 输出
把证据链和根因写到 .hermes/investigations/rundetail-no-tabs.md
