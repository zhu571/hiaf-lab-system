翻译 web-ui/src/views/SensorsView.vue。这个文件的中文全部替换为 t('sensors.xxx')。

步骤：
1. 加 import { useI18n } from 'vue-i18n' 和 const { t } = useI18n()
2. 各中文替换：
   - <h2>传感器数据</h2> → <h2>{{ t('sensors.title') }}</h2>
   - 自动刷新 → 用 $t('sensors.autoRefresh')
   - 重试（两处） → $t('sensors.retry')
   - 最新读数 → $t('sensors.latest')
   - 暂无读数 → $t('sensors.noReadings')
   - 历史趋势 → $t('sensors.history')
   - 各序列独立归一化 → $t('sensors.historyHint')
   - 所选时间范围内暂无数据 → $t('sensors.noDataInRange')
   - 最近 1 小时/最近 6 小时/最近 24 小时/最近 7 天 → $t('sensors.range.1h') 等
   - 压力/真空/控制/温度/泵 → $t('sensors.measurement.pressure') 等
   - script 中的中文消息 → t('sensors.xxx')

3. MEASUREMENTS 和 RANGES 改为 computed(() => [...]) 让 label 用 t() 调用

4. 在 zh.ts 和 en.ts 的 sensors 段加上缺的 key

5. npm run build 验证
