实现仪器执行结果可视化和保存测试数据的前端。

后端已完成：POST /api/v1/instruments/{id}/parse-result 接收 {command, response} 返回 {type, points, value, x_label, y_label}。

## 改动 web-ui/src/views/InstrumentMeasureView.vue

### 1. 导入 Chart.js（文件顶部已有 chart.js 引用，检查是否已注册所需组件）
检查现有 chart.js 注册，补上 ScatterController 和相关的元素注册。

### 2. cmdResult 区块改造（约 73-76 行）
当前：
```
<div v-if="cmdResult" class="cmd-result">
  <pre v-if="cmdResult.response" class="cmd-response">{{ cmdResult.response }}</pre>
  <p class="muted">命令 {{ cmdResult.command }} 完成，耗时...</p>
</div>
```

改为：
- 执行命令后自动调 parseResult(cmdResult.command, cmdResult.response)
- 如果 parsed.type === 'sweep_xy'：渲染 Chart.js 折线图（x: 频率, y: S11）
  - 图表下方显示 x_label / y_label
  - 如果命令含 "min" 或 "max"，在图上标注对应点
- 如果 parsed.type === 'single_value'：显示大号数值
- 始终显示原始 response（pre 块）
- 增加「保存到测试数据」按钮（viewer 隐藏）

### 3. 保存对话框
点击「保存到测试数据」弹出 el-dialog：
- 项目下拉（loadProjects）
- 批次下拉（选择项目后 listRuns，clearable）
- 数据类型（cryo/pressure/voltage/rf_voltage/efficiency）
- 测量项（预填 cmdResult.command，可改）
- 数值（预填 parsed.value，可改）
- 单位（自由输入）
- 测量时间（el-date-picker，默认现在）
- 备注（自动填入 "instrument={id} command={command}"）

注意：对话框放在 InstrumentMeasureView 内（不在项目路由下），所以需要手动调用 loadProjects 和 listRuns。

### 4. AI 对话区域的保存
AI 对话的每步执行结果（cmdCandidate 的 executed 状态）也加同样的「保存」按钮，复用同一个保存对话框。

提示：实际命令执行时，前端已有 cmdResult.command 和 cmdResult.response。parseResult 调用拿到结构化数据后渲染图表。保存时调 createTestData API。

验证：npm run build
