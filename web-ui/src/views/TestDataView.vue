<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('testData.title') }}</h2>
    </div>

    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane v-if="!isViewer" :label="t('testData.entry')" name="entry">
        <section class="panel">
          <h3 class="panel-title">{{ t('testData.entryTitle') }}</h3>
          <TestDataBatchEditor :project-id="projectId" :runs="runs" @submitted="onBatchSubmitted" />
        </section>
      </el-tab-pane>

      <el-tab-pane :label="t('testData.list')" name="list">
        <div class="tab-stack">
          <section class="panel filters-panel">
            <div class="filters">
              <el-select v-model="dataType" :placeholder="t('testData.dataType')" @change="onFilter">
                <el-option :label="t('testData.allTypes')" value="" />
                <el-option v-for="t in dataTypes" :key="t" :label="t" :value="t" />
              </el-select>
              <el-select v-model="quality" :placeholder="t('testData.quality')" @change="onFilter">
                <el-option :label="t('testData.allQualities')" value="" />
                <el-option v-for="q in qualities" :key="q" :label="q" :value="q" />
              </el-select>
            </div>
          </section>

          <section class="panel">
            <StateBlock
              :loading="loading && !items"
              :error="error"
              :empty="!items?.length"
              :error-text="t('testData.loadFailed')"
              :empty-text="t('testData.empty')"
              @retry="run"
            >
              <ResponsiveTable :rows="items ?? []" :loading="loading">
                <el-table-column :label="t('testData.measuredAt')" width="170">
                  <template #default="{ row }">{{ formatDateTime(row.measured_at) }}</template>
                </el-table-column>
                <el-table-column prop="data_type" :label="t('testData.dataType')" width="110" />
                <el-table-column prop="measurement" :label="t('testData.measurement')" min-width="140" />
                <el-table-column :label="t('testData.value')" width="130">
                  <template #default="{ row }">{{ row.value }}{{ row.unit ? ` ${row.unit}` : '' }}</template>
                </el-table-column>
                <el-table-column :label="t('testData.quality')" width="100">
                  <template #default="{ row }">
                    <StatusBadge domain="testQuality" :value="row.quality" />
                  </template>
                </el-table-column>
                <el-table-column prop="source" :label="t('testData.source')" width="100" />
                <el-table-column :label="t('testData.notes')" min-width="140" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.notes || '—' }}</template>
                </el-table-column>
                <el-table-column v-if="!isViewer" :label="t('testData.actions')" width="110">
                  <template #default="{ row }">
                    <el-button size="small" type="danger" plain :disabled="row.quality === 'invalid'" @click="invalidate(row)">{{ t('testData.markInvalid') }}</el-button>
                  </template>
                </el-table-column>
                <template #card="{ row }">
                  <div class="td-card">
                    <span class="card-title">{{ row.measurement }}</span>
                    <div class="card-fields">
                      <span>{{ row.data_type }}</span>
                      <span>{{ formatDateTime(row.measured_at) }}</span>
                      <span>{{ row.value }}{{ row.unit ? ` ${row.unit}` : '' }}</span>
                      <StatusBadge domain="testQuality" :value="row.quality" />
                    </div>
                    <div class="card-actions">
                      <el-button v-if="!isViewer" size="small" type="danger" plain :disabled="row.quality === 'invalid'" @click="invalidate(row)">{{ t('testData.markInvalid') }}</el-button>
                    </div>
                  </div>
                </template>
              </ResponsiveTable>
              <el-pagination
                v-model:current-page="page"
                v-model:page-size="perPage"
                class="pager"
                layout="total, prev, pager, next"
                :total="total"
                @current-change="run"
                @size-change="(n: number) => { onSizeChange(n); run() }"
              />
            </StateBlock>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="t('testData.chart')" name="chart">
        <section class="panel chart-panel">
          <h3 class="panel-title">{{ t('testData.chart') }} <span class="muted hint">{{ t('testData.chartHint') }}</span></h3>
          <template v-if="chartGroups.length">
            <div class="chart-scroll">
            <svg class="trend-chart" :viewBox="`0 0 ${CHART_W} ${CHART_H}`" preserveAspectRatio="xMidYMid meet">
              <line v-for="y in GRID_Y" :key="y" class="grid-line" :x1="PAD_X" :y1="y" :x2="CHART_W - PAD_X" :y2="y" />
              <text v-for="tick in yTicks" :key="tick.y" class="tick" :x="PAD_X - 4" :y="tick.y" text-anchor="end" dominant-baseline="middle">{{ tick.label }}</text>
              <line class="axis" :x1="PAD_X" :y1="CHART_H - PAD_Y" :x2="CHART_W - PAD_X" :y2="CHART_H - PAD_Y" />
              <line class="axis" :x1="PAD_X" :y1="PAD_Y" :x2="PAD_X" :y2="CHART_H - PAD_Y" />
              <g v-for="group in chartGroups" :key="group.name">
                <polyline
                  v-if="group.points.length >= 2"
                  :points="polyline(group)"
                  :stroke="group.color"
                  fill="none"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                />
                <circle v-for="(c, i) in chartCoords(group)" :key="i" class="dot" :cx="c.x" :cy="c.y" :fill="group.color" r="2" />
              </g>
            </svg>
            </div>
            <div class="legend">
              <span v-for="group in chartGroups" :key="group.name" class="legend-item">
                <i class="legend-dot" :style="{ background: group.color }" />{{ group.name }}
              </span>
            </div>
          </template>
          <el-empty v-else :description="t('testData.empty')" />
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { deleteTestData, listTestData, type TestData } from '@/api/testdata'
import { listRuns, type ExperimentRun } from '@/api/runs'
import { useAuthStore } from '@/stores/auth'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import { useTheme } from '@/composables/useTheme'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import TestDataBatchEditor from '@/components/business/TestDataBatchEditor.vue'
import { buildChartGroups, chartPalette, type ChartGroup } from '@/utils/chartTheme'
import { formatDateTime } from '@/utils/datetime'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()

const runs = ref<ExperimentRun[]>([])
const dataType = ref('')
const quality = ref('')
// 分页状态收敛到 usePagination（保持原 perPage=20 与 el-pagination layout 不变）
const { page, perPage, total, onSizeChange } = usePagination({ perPage: 20 })

const dataTypes = ['cryo', 'pressure', 'voltage', 'rf_voltage', 'efficiency']
const qualities = ['normal', 'outlier', 'suspect', 'invalid']

const isViewer = computed(() => auth.user?.role === 'viewer')
// projectId source of truth is the route param (guaranteed by ProjectLayout)
const projectId = computed(() => String(route.params.id || ''))
// viewer has no entry permission, default to list tab
const activeTab = ref(isViewer.value ? 'list' : 'entry')

// Trend chart: viewBox coordinates; palette/grouping 收敛到 utils/chartTheme.ts（P15 去重）
const CHART_W = 640
const CHART_H = 240
// S4 网格刻度：PAD_X 28→44 为 y 轴刻度文字让位（11px 标签右对齐于 PAD_X-4）
const PAD_X = 44
const PAD_Y = 20
// 3 条横向网格虚线 y 坐标（绘图区四等分）
const GRID_Y = [1, 2, 3].map((k) => PAD_Y + (k * (CHART_H - 2 * PAD_Y)) / 4)

// themeState 仅作依赖登记（美术 §3.6 SVG 联动）：主题切换 → computed 重算 → chartPalette 取新主题计算色，
// 系列色经 :stroke/:fill/legend-dot 内联样式自动更新，无需重建 SVG
const { state: themeState } = useTheme()
const chartGroups = computed<ChartGroup[]>(() => {
  void themeState.value
  return buildChartGroups(
    (items.value ?? []).map((item) => {
      const time = new Date(item.measured_at || item.created_at).getTime()
      return { key: item.measurement, time: Number.isNaN(time) ? 0 : time, value: item.value }
    }),
    chartPalette()
  )
})

// S4 y 轴 min/max 刻度：取全部序列的全局值域（各序列按自身值域归一，刻度作整体读数参照）
const yTicks = computed(() => {
  const values = chartGroups.value.flatMap((g) => g.points.map((p) => p.value))
  if (values.length === 0) return []
  const fmt = (v: number) => (Number.isInteger(v) ? String(v) : String(Math.round(v * 100) / 100))
  return [
    { y: PAD_Y, label: fmt(Math.max(...values)) },
    { y: CHART_H - PAD_Y, label: fmt(Math.min(...values)) }
  ]
})

function chartCoords(group: ChartGroup) {
  const n = group.points.length
  const values = group.points.map((p) => p.value)
  const min = Math.min(...values)
  const max = Math.max(...values)
  return group.points.map((p, i) => ({
    x: n === 1 ? CHART_W / 2 : PAD_X + (i / (n - 1)) * (CHART_W - 2 * PAD_X),
    y: max === min ? CHART_H / 2 : CHART_H - PAD_Y - ((p.value - min) / (max - min)) * (CHART_H - 2 * PAD_Y)
  }))
}

function polyline(group: ChartGroup) {
  return chartCoords(group)
    .map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`)
    .join(' ')
}

// 列表数据收敛到 useAsyncData（竞态 seq + 卸载丢弃内建；对齐原 onMounted(load) / watch(projectId, load)）：
// immediate 首屏自动加载；error 只写 ref 走 StateBlock 三态，不再 toast（操作级错误仍走 showApiError）
const {
  data: items,
  loading,
  error,
  run
} = useAsyncData<TestData[]>(
  async () => {
    if (!projectId.value) return []
    try {
      const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
      if (dataType.value) params.data_type = dataType.value
      if (quality.value) params.quality = quality.value
      const res = await listTestData(projectId.value, params)
      total.value = res.total ?? 0
      return res.items ?? []
    } finally {
      // 保持原 load() 数据流：无论列表请求成败，结束后都刷新运行下拉（批量录入编辑器的 runs 数据源）
      await loadRuns()
    }
  },
  { watch: [projectId] }
)

async function loadRuns() {
  if (isViewer.value || !projectId.value) {
    runs.value = []
    return
  }
  try {
    const data = await listRuns(projectId.value, { per_page: 100 })
    runs.value = data.items ?? []
  } catch (err) {
    showApiError(err, t('testData.runsLoadFailed'))
  }
}

function onFilter() {
  page.value = 1
  run()
}

// 批量录入成功 → 刷新列表（批量编辑器内部已清空表格）
async function onBatchSubmitted() {
  await run()
}

async function invalidate(row: TestData) {
  try {
    await ElMessageBox.confirm(t('testData.invalidateConfirm'), t('testData.markInvalid'), {
      confirmButtonText: t('testData.confirm'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await deleteTestData(row.id)
    ElMessage.success(t('testData.invalidated'))
    await run()
  } catch (err) {
    showApiError(err, t('testData.invalidateFailed'))
  }
}
</script>

<style scoped>
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 16px;
}

.tab-stack {
  display: grid;
  gap: 20px;
}

.panel-title {
  font-size: var(--fs-title-sm);
  margin-bottom: 14px;
}

.hint {
  font-size: 12px;
  font-weight: 400;
}

.filters-panel {
  padding: 14px 20px;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.filters .el-select {
  width: 160px;
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.chart-panel {
  display: grid;
  gap: 4px;
}

.trend-chart {
  display: block;
  height: auto;
  width: 100%;
}

.axis {
  stroke: var(--border-strong);
  stroke-width: 1;
}

.grid-line {
  stroke: var(--border);
  stroke-dasharray: 3 4;
  stroke-width: 1;
}

.tick {
  fill: var(--text-3);
  font-size: 11px;
}

.dot:hover {
  r: 3;
}

@media (max-width: 768px) {
  .filters .el-select {
    width: 100%;
  }

  .chart-scroll {
    overflow-x: auto;
  }

  .trend-chart {
    width: 640px;
  }
}
</style>
