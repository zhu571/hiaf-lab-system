翻译以下页面的中文文字为英文。用 vue-i18n 的方式。

## 翻译方法

### 1. 往 zh.ts 加 key（如已有则跳过）
### 2. 往 en.ts 加对应英文翻译
### 3. 模板中把中文替换为 $t('key')
### 4. script 中把中文替换为 t('key')

## 本次翻译的页面

### 1. views/AssemblyView.vue
查找所有中文文字（按钮/标签/标题/提示/消息），替换为 $t。

需要加的 key 示例：
- assembly.aiGenerate -> "AI 生成步骤"
- assembly.fromTemplate -> "从模板导入"
- assembly.manualCreate -> "手动新建"
- assembly.stepName -> "步骤名称"
- assembly.apply -> "直接应用"
- assembly.saveTemplate -> "存模板"
- assembly.saveAndApply -> "存并应用"
- 等

### 2. views/GasControlView.vue
- gasControl.title -> "气压控制"
- gasControl.realtime -> "实时推送"
- gasControl.reconnecting -> "正在重连"
- gasControl.a1Trip -> "A5 联锁已触发"
- gasControl.setpoint -> "设定值"
- gasControl.valveOpening -> "阀门开度"
- gasControl.running -> "运行中"
- gasControl.stopped -> "已停止"
- gasControl.start -> "启动"
- gasControl.stop -> "停止"
- gasControl.setValve -> "设置阀位"
- 等

### 3. views/SensorsView.vue
- sensors.title -> "传感器数据"
- sensors.temperature -> "温度"
- sensors.pressure -> "压力"
- sensors.vacuum -> "真空"
- 等

### 4. views/TestDataView.vue
- testData.title -> "测试数据"
- testData.entry -> "录入"
- testData.dataType -> "数据类型"
- testData.measurement -> "测量项"
- testData.value -> "数值"
- testData.unit -> "单位"
- testData.quality -> "质量"
- 等

### 5. views/RunListView.vue
- runList.title -> "实验流程"
- runList.create -> "新建实验"
- runList.name -> "名称"
- runList.type -> "类型"
- runList.status -> "状态"
- 等

### 6. views/IssuesView.vue
- issues.title -> "问题管理"
- issues.create -> "新建问题"
- issues.unresolved -> "未解决"
- issues.inProgress -> "进行中"
- issues.resolved -> "已解决"
- 等

## 规则
- 不要改功能和逻辑
- 每个页面的 key 用页面名做前缀，避免冲突
- script 中的中文用 t() 函数，import { useI18n } from 'vue-i18n' 然后用 const { t } = useI18n()
- 模板中用 $t()
- element-plus 的 label/placeholder 属性用 :label="$t('key')"
- 翻译完成后验证 npm run build
