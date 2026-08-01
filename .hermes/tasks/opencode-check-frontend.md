仔细排查为什么前端看不到两个功能：
1. 实验批次详情页（RunDetailView）看不到「AI 生成步骤」按钮
2. 仪器测量页（InstrumentMeasureView）看不到曲线显示和「保存到测试数据」

用户访问 http://10.144.144.12:8000/，服务器已部署最新代码（已通过 curl 验证 chunk 含 aiGenerateSteps 和 sweep_xy）。

请检查以下可能性：

## 1. 源码层面（本地仓库 /home/zhuhaofan/hiaf-lab-system）
- web-ui/src/views/RunDetailView.vue：AI 生成步骤按钮是否真的在模板中、v-if 条件是什么（canEdit 如何计算）、tabs 结构是否正确
- web-ui/src/views/InstrumentMeasureView.vue：曲线渲染逻辑（Chart.js 是否注册、cmdResult 是否在正确位置）、「保存到测试数据」按钮的 v-if 条件
- web-ui/src/i18n/zh.ts 和 en.ts：runDetail.aiGenerateSteps、instrument.sweepCurve 等 key 是否存在

## 2. 前端路由
- router/index.ts：RunDetailView 和 InstrumentMeasureView 路由是否正常注册

## 3. API 层
- web-ui/src/api/runs.ts：listRunSteps/applyRunTemplate 是否导出
- web-ui/src/api/instruments.ts：parseResult 是否导出
- 后端 go-server 对应 handler 是否存在

## 4. 构建产物
- go-server/static/ 里 RunDetailView chunk 是否包含这些功能（当前是 BTVjzuW7.js）

## 5. 运行时
- gascell 上 lab-server 容器是否跑的是最新镜像
- 浏览器缓存问题（让用户硬刷新）之外，是否有其他可能

用代码证据逐条验证，输出结论到 .hermes/investigations/frontend-features-missing.md
