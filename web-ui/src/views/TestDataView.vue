<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ $t('testData.title') }}</h2>
    </div>

    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane v-if="!isViewer" :label="$t('testData.entry')" name="entry">
        <section class="panel">
          <h3 class="panel-title">{{ $t('testData.entryTitle') }}</h3>
          <el-form label-position="top" @submit.prevent>
            <div class="form-grid">
              <el-form-item :label="$t('testData.dataType')" required>
                <el-select v-model="draft.data_type" :placeholder="$t('testData.dataTypePlaceholder')">
                  <el-option v-for="t in dataTypes" :key="t" :label="t" :value="t" />
                </el-select>
              </el-form-item>
              <el-form-item :label="$t('testData.measurement')" required>
                <el-input v-model="draft.measurement" :placeholder="$t('testData.measurementPlaceholder')" />
              </el-form-item>
              <el-form-item :label="$t('testData.value')" required>
                <el-input-number v-model="draft.value" :controls="false" :placeholder="$t('testData.value')" />
              </el-form-item>
              <el-form-item :label="$t('testData.unit')">
                <el-input v-model="draft.unit" :placeholder="$t('testData.unitPlaceholder')" />
              </el-form-item>
              <el-form-item :label="$t('testData.quality')">
                <el-select v-model="draft.quality">
                  <el-option v-for="q in entryQualities" :key="q" :label="q" :value="q" />
                </el-select>
              </el-form-item>
              <el-form-item :label="$t('testData.measuredAt')">
                <el-date-picker v-model="draft.measured_at" type="datetime" :placeholder="$t('testData.timePlaceholder')" />
              </el-form-item>
              <el-form-item :label="$t('testData.linkedRun')">
                <el-select v-model="draft.run_id" :placeholder="$t('testData.runPlaceholder')" clearable>
                  <el-option v-for="r in runs" :key="r.id" :label="r.name" :value="r.id" />
                </el-select>
              </el-form-item>
              <el-form-item :label="$t('testData.notes')" class="span-all">
                <el-input v-model="draft.notes" :placeholder="$t('testData.notesPlaceholder')" />
              </el-form-item>
            </div>
            <div class="form-actions">
              <el-button type="primary" :loading="submitting" @click="submit">{{ $t('testData.submit') }}</el-button>
            </div>
          </el-form>
        </section>
      </el-tab-pane>

      <el-tab-pane :label="$t('testData.list')" name="list">
        <div class="tab-stack">
          <section class="panel filters-panel">
            <div class="filters">
              <el-select v-model="dataType" :placeholder="$t('testData.dataType')" @change="onFilter">
                <el-option :label="$t('testData.allTypes')" value="" />
                <el-option v-for="t in dataTypes" :key="t" :label="t" :value="t" />
              </el-select>
              <el-select v-model="quality" :placeholder="$t('testData.quality')" @change="onFilter">
                <el-option :label="$t('testData.allQualities')" value="" />
                <el-option v-for="q in qualities" :key="q" :label="q" :value="q" />
              </el-select>
            </div>
          </section>

          <section class="panel">
            <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
              <el-button size="small" @click="load">{{ $t('testData.retry') }}</el-button>
            </el-alert>
            <template v-else>
              <el-table v-loading="loading" :data="items">
                <el-table-column :label="$t('testData.measuredAt')" width="170">
                  <template #default="{ row }">{{ formatTime(row.measured_at) }}</template>
                </el-table-column>
                <el-table-column prop="data_type" :label="$t('testData.dataType')" width="110" />
                <el-table-column prop="measurement" :label="$t('testData.measurement')" min-width="140" />
                <el-table-column :label="$t('testData.value')" width="130">
                  <template #default="{ row }">{{ row.value }}{{ row.unit ? ` ${row.unit}` : '' }}</template>
                </el-table-column>
                <el-table-column :label="$t('testData.quality')" width="100">
                  <template #default="{ row }">
                    <el-tag :type="qualityTag(row.quality)" size="small" effect="light">{{ row.quality }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="source" :label="$t('testData.source')" width="100" />
                <el-table-column :label="$t('testData.notes')" min-width="140" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.notes || '—' }}</template>
                </el-table-column>
                <el-table-column v-if="!isViewer" :label="$t('testData.actions')" width="110">
                  <template #default="{ row }">
                    <el-button size="small" type="danger" plain :disabled="row.quality === 'invalid'" @click="invalidate(row)">{{ $t('testData.markInvalid') }}</el-button>
                  </template>
                </el-table-column>
                <template #empty>
                  <el-empty :description="$t('testData.empty')" />
                </template>
              </el-table>
              <el-pagination
                v-model:current-page="page"
                class="pager"
                layout="total, prev, pager, next"
                :page-size="perPage"
                :total="total"
                @current-change="load"
              />
            </template>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="$t('testData.chart')" name="chart">
        <section class="panel chart-panel">
          <h3 class="panel-title">{{ $t('testData.chart') }} <span class="muted hint">{{ $t('testData.chartHint') }}</span></h3>
          <template v-if="chartGroups.length">
            <svg class="trend-chart" :viewBox="`0 0 ${CHART_W} ${CHART_H}`" preserveAspectRatio="xMidYMid meet">
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
                <circle v-for="(c, i) in chartCoords(group)" :key="i" :cx="c.x" :cy="c.y" :fill="group.color" r="3" />
              </g>
            </svg>
            <div class="legend">
              <span v-for="group in chartGroups" :key="group.name" class="legend-item">
                <i class="legend-dot" :style="{ background: group.color }" />{{ group.name }}
              </span>
            </div>
          </template>
          <el-empty v-else :description="$t('testData.empty')" />
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createTestData, deleteTestData, listTestData, type TestData, type TestDataPayload } from '../api/testdata'
import { listRuns, type ExperimentRun } from '../api/runs'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()

const items = ref<TestData[]>([])
const runs = ref<ExperimentRun[]>([])
const loading = ref(false)
const submitting = ref(false)
const error = ref('')
const page = ref(1)
const perPage = 20
const total = ref(0)
const dataType = ref('')
const quality = ref('')

const dataTypes = ['cryo', 'pressure', 'voltage', 'rf_voltage', 'efficiency']
const entryQualities = ['normal', 'outlier', 'suspect']
const qualities = ['normal', 'outlier', 'suspect', 'invalid']

const draft = reactive({
  data_type: '',
  measurement: '',
  value: undefined as number | undefined,
  unit: '',
  quality: 'normal',
  measured_at: null as Date | null,
  run_id: '',
  notes: ''
})

const isViewer = computed(() => auth.user?.role === 'viewer')
// projectId source of truth is the route param (guaranteed by ProjectLayout)
const projectId = computed(() => String(route.params.id || ''))
// viewer has no entry permission, default to list tab
const activeTab = ref(isViewer.value ? 'list' : 'entry')

// Trend chart: viewBox coordinates and palette
const CHART_W = 640
const CHART_H = 240
const PAD_X = 28
const PAD_Y = 20
const palette = ['var(--brand-500)', 'var(--ok)', 'var(--warn)', 'var(--danger)', '#7a5af8', '#0d5a70', '#c2477e', '#5a8a3c']

type ChartPoint = { time: number; value: number }
type ChartGroup = { name: string; color: string; points: ChartPoint[] }

const chartGroups = computed<ChartGroup[]>(() => {
  const groups = new Map<string, ChartPoint[]>()
  for (const item of items.value) {
    const time = new Date(item.measured_at || item.created_at).getTime()
    const list = groups.get(item.measurement) || []
    list.push({ time: Number.isNaN(time) ? 0 : time, value: item.value })
    groups.set(item.measurement, list)
  }
  return Array.from(groups.entries()).map(([name, points], index) => ({
    name,
    color: palette[index % palette.length],
    points: points.sort((a, b) => a.time - b.time)
  }))
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

onMounted(load)
watch(projectId, load)

async function load() {
  if (!projectId.value) return
  loading.value = true
  error.value = ''
  try {
    const params: Record<string, string | number> = { page: page.value, per_page: perPage }
    if (dataType.value) params.data_type = dataType.value
    if (quality.value) params.quality = quality.value
    const data = await listTestData(projectId.value, params)
    items.value = data.items ?? []
    total.value = data.total
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('testData.loadFailed')
    showApiError(err, t('testData.loadFailed'))
  } finally {
    loading.value = false
  }
  await loadRuns()
}

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
  load()
}

function resetDraft() {
  draft.data_type = ''
  draft.measurement = ''
  draft.value = undefined
  draft.unit = ''
  draft.quality = 'normal'
  draft.measured_at = null
  draft.run_id = ''
  draft.notes = ''
}

async function submit() {
  if (!draft.data_type) {
    ElMessage.warning(t('testData.dataTypeRequired'))
    return
  }
  if (!draft.measurement.trim()) {
    ElMessage.warning(t('testData.measurementRequired'))
    return
  }
  if (draft.value === undefined || Number.isNaN(draft.value)) {
    ElMessage.warning(t('testData.valueRequired'))
    return
  }
  // backend enables DisallowUnknownFields, only submit whitelisted fields
  const payload: TestDataPayload = {
    data_type: draft.data_type,
    measurement: draft.measurement.trim(),
    value: draft.value,
    quality: draft.quality
  }
  const unit = draft.unit.trim()
  if (unit) payload.unit = unit
  if (draft.measured_at) payload.measured_at = new Date(draft.measured_at).toISOString()
  if (draft.run_id) payload.run_id = draft.run_id
  const notes = draft.notes.trim()
  if (notes) payload.notes = notes
  submitting.value = true
  try {
    await createTestData(projectId.value, payload)
    ElMessage.success(t('testData.created'))
    resetDraft()
    await load()
  } catch (err) {
    showApiError(err, t('testData.createFailed'))
  } finally {
    submitting.value = false
  }
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
    await load()
  } catch (err) {
    showApiError(err, t('testData.invalidateFailed'))
  }
}

function qualityTag(v: string): 'success' | 'warning' | 'danger' | 'info' {
  if (v === 'normal') return 'success'
  if (v === 'outlier') return 'warning'
  if (v === 'suspect') return 'danger'
  return 'info'
}

function formatTime(v?: string) {
  return v ? new Date(v).toLocaleString('zh-CN', { hour12: false }) : '—'
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
  font-size: 15px;
  margin-bottom: 14px;
}

.hint {
  font-size: 12px;
  font-weight: 400;
}

.form-grid {
  display: grid;
  gap: 0 14px;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
}

.form-grid .el-select,
.form-grid .el-input-number,
.form-grid .el-date-editor {
  width: 100%;
}

.span-all {
  grid-column: 1 / -1;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
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

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin-top: 10px;
}

.legend-item {
  align-items: center;
  color: var(--text-2);
  display: inline-flex;
  font-size: 12px;
  gap: 6px;
}

.legend-dot {
  border-radius: 50%;
  display: inline-block;
  height: 8px;
  width: 8px;
}

@media (max-width: 768px) {
  .filters .el-select {
    width: 100%;
  }
}
</style>
