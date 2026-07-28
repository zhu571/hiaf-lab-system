SensorsView.vue 已经部分翻译了，就差这 6 处：

1. line 41: description="暂无读数" → :description="t('sensors.noReadings')"
2. line 49: <h3 class="panel-title">历史趋势 <span class="muted hint">各序列独立归一化</span></h3> 
   改为 <h3 class="panel-title">{{ t('sensors.history') }} <span class="muted hint">{{ t('sensors.historyHint') }}</span></h3>
3. line 60: >重试< → >{{ $t('sensors.retry') }}<
4. line 88: description="所选时间范围内暂无数据" → :description="t('sensors.noDataInRange')"
5. script 中 MEASUREMENTS 和 RANGES 改为 computed，label 用 t()
6. script 中错误消息 '最新读数加载失败' 和 '历史数据加载失败' 改为 t('sensors.loadLatestFailed') 和 t('sensors.loadHistoryFailed')

补 zh.ts/en.ts 的 sensors.loadLatestFailed 和 sensors.loadHistoryFailed。

改完后 npm run build 验证。
