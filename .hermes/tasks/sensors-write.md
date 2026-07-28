用 Write 重写 web-ui/src/views/SensorsView.vue。把全部中文替换为 i18n key。

需要改的地方：
1. 加 import { useI18n } from 'vue-i18n' 和 const { t } = useI18n()
2. 所有中文模板文字改成 {{ t('sensors.xxx') }}
3. MEASUREMENTS 数组改为 computed，label 用 t()
4. RANGES 数组改为 computed，label 用 t()
5. script 中中文改成 t()
6. zh.ts/en.ts 补 sensors 缺的 key

zh.ts sensors 段现有的 key：
- title: 传感器数据, autoRefresh: 自动刷新, retry: 重试, refresh: 刷新,
  latest: 最新读数, noReadings: 暂无读数, history: 历史趋势, historyHint: 各序列独立归一化,
  noDataInRange: 所选时间范围内暂无数据,
  range: {1h: '最近 1 小时', 6h: '最近 6 小时', 24h: '最近 24 小时', 7d: '最近 7 天'},
  measurement: {pressure: '压力', vacuum: '真空', control: '控制', temperature: '温度', pump: '泵'}

完成后 npm run build 验证。
