<template>
  <div class="electrical-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">Electrical</p>
        <h1>{{ t('electrical.title') }}</h1>
        <p class="subtitle">{{ t('electrical.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <div class="monitor-actions">
          <label class="continuous-control">
            <el-switch v-model="continuousView" :aria-label="t('electrical.continuousView')" />
            <span>{{ t('electrical.continuousView') }}</span>
          </label>
          <el-button :loading="refreshing" @click="refreshLatest">{{ t('electrical.refresh') }}</el-button>
          <el-button type="primary" :loading="shotRunning" @click="runSingleShot">{{ t('electrical.singleShot') }}</el-button>
        </div>
        <p class="continuous-hint">{{ t('electrical.continuousHint') }}</p>
      </div>
    </header>

    <StateBlock :loading="loading && !latest" :error="latest ? null : loadError" :error-text="errorMessage" @retry="refreshLatest">
      <div v-if="latest" class="monitor-content">
        <section class="overview">
          <div class="overview-status">
            <el-tag :type="latest.continuous_enabled ? 'success' : 'info'">
              {{ latest.continuous_enabled ? t('electrical.backendContinuous') : t('electrical.backendOnDemand') }}
            </el-tag>
            <span>{{ t('electrical.updatedAt', { time: formatDateTime(latest.updated_at) }) }}</span>
          </div>
          <div class="storage-status">
            <el-tag :type="latest.write_backlog > 0 ? 'warning' : 'info'" effect="plain">
              {{ t('electrical.writeBacklog', { count: latest.write_backlog }) }}
            </el-tag>
            <el-tag :type="totalWriteErrors > 0 ? 'danger' : 'success'" effect="plain">
              {{ t('electrical.writeErrors', { count: totalWriteErrors }) }}
            </el-tag>
          </div>
        </section>

        <el-alert v-if="loadError" :title="errorMessage" type="warning" :closable="false" show-icon />
        <el-empty v-if="allSamples.length === 0" :description="t('electrical.empty')" />

        <div class="sources-grid">
          <article v-for="section in sourceSections" :key="section.source" class="source-card" :class="{ 'mestek-card': section.source === 'mestek' }">
            <header class="source-header">
              <div class="source-heading">
                <div class="source-title">
                  <h2>{{ sourceLabel(section.source) }}</h2>
                  <el-tag size="small" effect="plain">{{ modeLabel(section.mode) }}</el-tag>
                </div>
                <el-tag v-if="section.source === 'mestek'" size="small" :type="section.health?.mapping === 'request_response' ? 'success' : 'warning'">
                  {{ mappingLabel(section.health?.mapping || section.samples[0]?.mapping) }}
                </el-tag>
              </div>
              <StatusBadge domain="electricalHealth" :value="section.health?.status || 'connecting'" />
            </header>

            <div class="source-meta">
              <span>{{ section.health?.last_sample_at ? t('electrical.lastSample', { time: formatDateTime(section.health.last_sample_at) }) : t('electrical.noSample') }}</span>
              <span>{{ t('electrical.sampleCount', { count: section.health?.sample_count ?? 0 }) }}</span>
              <span>{{ t('electrical.sourceErrors', { count: section.health?.error_count ?? 0 }) }}</span>
              <span>{{ t('electrical.writeErrors', { count: section.health?.write_errors ?? 0 }) }}</span>
            </div>
            <p v-if="section.source === 'mestek'" class="frame-stats">
              {{ t('electrical.frameStats', {
                requests: section.health?.request_frames ?? 0,
                responses: section.health?.response_frames ?? 0,
                crc: section.health?.crc_errors ?? 0,
                pairing: section.health?.pairing_errors ?? 0
              }) }}
            </p>
            <div v-if="section.health?.last_error || section.health?.storage_error" class="source-errors">
              <p v-if="section.health.last_error"><strong>{{ t('electrical.acquisitionError') }}:</strong> {{ section.health.last_error }}</p>
              <p v-if="section.health.storage_error"><strong>{{ t('electrical.storageError') }}:</strong> {{ section.health.storage_error }}</p>
            </div>

            <el-table v-if="section.samples.length" :data="section.samples" :row-key="sampleKey" size="small" :max-height="360">
              <el-table-column prop="channel" :label="t('electrical.channel')" width="110" />
              <el-table-column :label="t('electrical.metric')" min-width="145">
                <template #default="{ row }">{{ metricLabel(row.metric) }}</template>
              </el-table-column>
              <el-table-column :label="t('electrical.value')" min-width="180">
                <template #default="{ row }">
                  <span class="sample-value" :class="{ raw: row.metric === 'frame' }" :title="row.text_value || row.raw">{{ displayValue(row) }}</span>
                </template>
              </el-table-column>
              <el-table-column v-if="section.source === 'mestek'" :label="t('electrical.mapping')" min-width="170">
                <template #default="{ row }">{{ mappingLabel(row.mapping) }}</template>
              </el-table-column>
              <el-table-column :label="t('electrical.quality')" width="80">
                <template #default="{ row }">{{ qualityLabel(row.quality) }}</template>
              </el-table-column>
              <el-table-column :label="t('electrical.time')" width="165">
                <template #default="{ row }">{{ formatDateTime(row.time) }}</template>
              </el-table-column>
            </el-table>
            <p v-else-if="allSamples.length" class="source-empty">{{ t('electrical.noSample') }}</p>
          </article>
        </div>

        <section class="trend-card">
          <div class="section-title">
            <div>
              <h2>{{ t('electrical.trendTitle') }}</h2>
              <p>{{ t('electrical.trendHint') }}</p>
            </div>
          </div>
          <div class="trend-controls">
            <el-select v-model="trendSource" :aria-label="t('electrical.trendSource')" :disabled="!trendSources.length" class="trend-source">
              <el-option v-for="source in trendSources" :key="source" :label="sourceLabel(source)" :value="source" />
            </el-select>
            <el-select v-model="trendMetric" :aria-label="t('electrical.trendMetric')" :disabled="!trendMetricOptions.length" class="trend-metric">
              <el-option v-for="option in trendMetricOptions" :key="option.value" :label="option.label" :value="option.value" />
            </el-select>
          </div>
          <SensorTrendChart v-if="trendGroups.length" :groups="trendGroups" :window-key="trendWindowKey" :window-size-ms="TREND_WINDOW_MS" />
          <el-empty v-else :description="t('electrical.noTrend')" :image-size="72" />
        </section>
      </div>
    </StateBlock>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  getElectricalLatest,
  singleShotElectrical,
  type ElectricalLatest,
  type ElectricalSample,
  type ElectricalSingleShotResult
} from '../api/electrical'
import { isApiError } from '../api/client'
import { usePolling } from '../composables/usePolling'
import { showApiError } from '../composables/useNotify'
import { useTheme } from '../composables/useTheme'
import SensorTrendChart from '@/components/business/SensorTrendChart.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { chartPalette, LINE_DASHES, type ChartGroup, type ChartPoint } from '@/utils/chartTheme'
import { formatDateTime } from '@/utils/datetime'

const { t } = useI18n()
const TREND_WINDOW_MS = 10 * 60 * 1000
const TREND_POINT_LIMIT = 120

const SOURCE_DEFINITIONS = [
  { source: 'mestek', labelKey: 'electrical.sources.mestek', mode: 'passive' },
  { source: 'keithley6485', labelKey: 'electrical.sources.keithley6485', mode: 'passive' },
  { source: 'rigol_dho814', labelKey: 'electrical.sources.rigol_dho814', mode: 'scpi' },
  { source: 'tek_mdo3034', labelKey: 'electrical.sources.tek_mdo3034', mode: 'scpi' },
  { source: 'agilent_33512b', labelKey: 'electrical.sources.agilent_33512b', mode: 'scpi' },
  { source: 'agilent_33210a', labelKey: 'electrical.sources.agilent_33210a', mode: 'scpi' }
] as const

const METRIC_LABEL_KEYS: Record<string, string> = {
  output_enabled: 'electrical.metrics.output_enabled',
  U_V: 'electrical.metrics.U_V',
  I_A: 'electrical.metrics.I_A',
  setU_V: 'electrical.metrics.setU_V',
  frame: 'electrical.metrics.frame',
  current_A: 'electrical.metrics.current_A',
  vpp_V: 'electrical.metrics.vpp_V',
  measurement_value: 'electrical.metrics.measurement_value',
  type: 'electrical.metrics.type',
  source: 'electrical.metrics.source',
  waveform: 'electrical.metrics.waveform',
  frequency_Hz: 'electrical.metrics.frequency_Hz',
  amplitude_V: 'electrical.metrics.amplitude_V',
  offset_V: 'electrical.metrics.offset_V'
}

const latest = ref<ElectricalLatest>()
const continuousView = ref(true)
const loading = ref(true)
const refreshing = ref(false)
const shotRunning = ref(false)
const loadError = ref<Error | null>(null)

const allSamples = computed(() => latest.value?.samples ?? [])
const totalWriteErrors = computed(() => (latest.value?.sources ?? []).reduce((total, source) => total + source.write_errors, 0))
const errorMessage = computed(() => loadError.value ? `${t('electrical.loadFailed')}: ${loadError.value.message}` : '')
const sourceSections = computed(() => SOURCE_DEFINITIONS.map((definition) => ({
  ...definition,
  health: latest.value?.sources?.find((health) => health.source === definition.source),
  samples: allSamples.value
    .filter((sample) => sample.source === definition.source)
    .sort((a, b) => a.channel.localeCompare(b.channel, undefined, { numeric: true }) || a.metric.localeCompare(b.metric) || (a.signature || '').localeCompare(b.signature || ''))
})))

const polling = usePolling(refreshLatest, 2000)
onMounted(() => {
  void refreshLatest()
  if (continuousView.value) polling.start()
})
watch(continuousView, (enabled) => {
  if (enabled) {
    void refreshLatest()
    polling.start()
  } else {
    polling.stop()
  }
})

async function refreshLatest() {
  if (refreshing.value) return
  refreshing.value = true
  if (!latest.value) loading.value = true
  try {
    const result = await getElectricalLatest()
    latest.value = result
    loadError.value = null
    recordTrend(result.samples ?? [])
  } catch (error) {
    loadError.value = error instanceof Error ? error : new Error(String(error))
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function runSingleShot() {
  if (shotRunning.value) return
  shotRunning.value = true
  try {
    const { data, requestId } = await singleShotElectrical()
    mergeSingleShot(data)
    recordTrend(data.samples ?? [])
    ElMessage.success(t('electrical.singleShotSuccess', { count: data.samples?.length ?? 0, requestId }))
    const errorCount = Object.keys(data.errors ?? {}).length
    if (errorCount) ElMessage.warning(t('electrical.singleShotPartial', { count: errorCount }))
    await refreshLatest()
  } catch (error) {
    if (isApiError(error) && error.status === 409) {
      showOperationError(t('electrical.singleShotConflict'), error)
    } else if (isApiError(error) && error.status === 429) {
      const details = error.details as { retry_after_seconds?: unknown } | undefined
      const seconds = typeof details?.retry_after_seconds === 'number' ? details.retry_after_seconds : 0
      showOperationError(t(seconds > 0 ? 'electrical.singleShotRateLimited' : 'electrical.singleShotRateLimitedUnknown', { seconds }), error)
    } else if (isApiError(error) && error.status === 504) {
      showOperationError(t('electrical.singleShotTimeout'), error)
    } else {
      showApiError(error, t('electrical.singleShotFailed'))
    }
  } finally {
    shotRunning.value = false
  }
}

function showOperationError(message: string, error: { requestId?: string }) {
  ElMessage.error(error.requestId ? `${message}（request_id: ${error.requestId}）` : message)
}

function mergeSingleShot(result: ElectricalSingleShotResult) {
  if (!latest.value) return
  const samples = new Map(allSamples.value.map((sample) => [sampleKey(sample), sample]))
  for (const sample of result.samples ?? []) samples.set(sampleKey(sample), sample)
  latest.value = {
    ...latest.value,
    updated_at: result.finished_at,
    sources: result.sources ?? latest.value.sources,
    samples: Array.from(samples.values())
  }
}

function sampleKey(sample: ElectricalSample) {
  return [sample.source, sample.channel, sample.metric, sample.mapping, sample.signature || ''].join('\u0000')
}

function sourceLabel(source: string) {
  const definition = SOURCE_DEFINITIONS.find((item) => item.source === source)
  return definition ? t(definition.labelKey) : source
}

function modeLabel(mode: 'passive' | 'scpi') {
  return t(mode === 'passive' ? 'electrical.passiveReadOnly' : 'electrical.scpiReadOnly')
}

function metricLabel(metric: string) {
  const key = METRIC_LABEL_KEYS[metric]
  return key ? t(key) : metric
}

function mappingLabel(mapping?: string) {
  if (mapping === 'request_response') return t('electrical.mappingPaired')
  if (mapping === 'unmapped') return t('electrical.mappingUnmapped')
  if (mapping === 'direct') return t('electrical.mappingDirect')
  return t('electrical.mappingUnknown')
}

function qualityLabel(quality: string) {
  return quality === 'good' ? t('electrical.qualityGood') : quality || '—'
}

function displayValue(sample: ElectricalSample) {
  if (typeof sample.value === 'number' && Number.isFinite(sample.value)) {
    if (sample.metric === 'output_enabled') return sample.value === 0 ? t('electrical.outputOff') : t('electrical.outputOn')
    const value = formatNumber(sample.value)
    return sample.unit ? `${value} ${sample.unit}` : value
  }
  return sample.text_value || '—'
}

function formatNumber(value: number) {
  if (value === 0) return '0'
  const absolute = Math.abs(value)
  if (absolute >= 1e6 || absolute < 1e-3) return value.toExponential(3)
  return String(Number(value.toPrecision(6)))
}

type SeriesMeta = Pick<ElectricalSample, 'source' | 'channel' | 'metric' | 'signature'>
const trendHistory = ref<Record<string, ChartPoint[]>>({})
const seriesMeta: Record<string, SeriesMeta> = {}
const lastMarkers = new Map<string, string>()
const trendSource = ref('')
const trendMetric = ref('')
const { state: themeState } = useTheme()

function recordTrend(samples: ElectricalSample[]) {
  const next = { ...trendHistory.value }
  let changed = false
  for (const sample of samples) {
    if (typeof sample.value !== 'number' || !Number.isFinite(sample.value)) continue
    const time = Date.parse(sample.time)
    if (!Number.isFinite(time)) continue
    const key = sampleKey(sample)
    const marker = `${sample.seq}:${sample.time}`
    if (lastMarkers.get(key) === marker) continue
    lastMarkers.set(key, marker)
    seriesMeta[key] = { source: sample.source, channel: sample.channel, metric: sample.metric, signature: sample.signature }
    next[key] = [...(next[key] ?? []), { time, value: sample.value }].slice(-TREND_POINT_LIMIT)
    changed = true
  }
  if (changed) trendHistory.value = next
}

const numericSamples = computed(() => allSamples.value.filter((sample) => typeof sample.value === 'number' && Number.isFinite(sample.value)))
const trendSources = computed(() => SOURCE_DEFINITIONS.map((item) => item.source).filter((source) => numericSamples.value.some((sample) => sample.source === source)))
const trendMetricOptions = computed(() => {
  const metrics = new Map<string, string>()
  for (const sample of numericSamples.value) {
    if (sample.source !== trendSource.value || metrics.has(sample.metric)) continue
    metrics.set(sample.metric, `${metricLabel(sample.metric)}${sample.unit ? ` (${sample.unit})` : ''}`)
  }
  return Array.from(metrics, ([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label))
})

watch(trendSources, (sources) => {
  if (!sources.includes(trendSource.value as typeof sources[number])) trendSource.value = sources[0] ?? ''
}, { immediate: true })
watch(trendMetricOptions, (options) => {
  if (options.some((option) => option.value === trendMetric.value)) return
  const preferred = ['U_V', 'current_A', 'vpp_V', 'measurement_value', 'frequency_Hz', 'amplitude_V', 'output_enabled']
  trendMetric.value = preferred.find((metric) => options.some((option) => option.value === metric)) ?? options[0]?.value ?? ''
}, { immediate: true })

const trendGroups = computed<ChartGroup[]>(() => {
  void themeState.value
  const palette = chartPalette()
  return Object.entries(trendHistory.value)
    .filter(([key]) => {
      const meta = seriesMeta[key]
      return meta?.source === trendSource.value && meta.metric === trendMetric.value
    })
    .map(([key, points]) => {
      const meta = seriesMeta[key]
      const name = meta.channel === 'UNMAPPED' && meta.signature ? `${meta.channel} · ${meta.signature.slice(0, 6)}` : meta.channel
      return { key, name, points }
    })
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }))
    .map((series, index) => ({
      name: series.name,
      color: palette[index % palette.length],
      dash: LINE_DASHES[index % LINE_DASHES.length],
      points: series.points
    }))
})
const trendWindowKey = computed(() => `${trendSource.value}:${trendMetric.value}`)
</script>

<style scoped>
.electrical-page,
.monitor-content {
  display: grid;
  gap: 20px;
}

.page-header,
.overview,
.source-header,
.section-title {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.page-header {
  align-items: flex-start;
  gap: 20px;
}

.page-header h1,
.source-title h2,
.section-title h2 {
  margin: 0;
}

.eyebrow {
  color: var(--brand-600);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: .12em;
  margin: 0 0 4px;
  text-transform: uppercase;
}

.subtitle,
.section-title p,
.continuous-hint {
  color: var(--text-3);
  margin: 5px 0 0;
}

.header-actions {
  display: grid;
  gap: 6px;
  justify-items: end;
}

.monitor-actions,
.continuous-control,
.overview-status,
.storage-status,
.source-title,
.source-meta,
.trend-controls {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.continuous-control {
  cursor: pointer;
  gap: 8px;
  min-height: var(--touch-target);
}

.monitor-actions :deep(.el-button) {
  min-height: var(--touch-target);
}

.continuous-hint {
  font-size: 12px;
  max-width: 520px;
  text-align: right;
}

.overview {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 14px;
  gap: 14px;
  padding: 14px 18px;
}

.overview-status {
  color: var(--text-3);
}

.sources-grid {
  align-items: start;
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.source-card,
.trend-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: var(--shadow-sm);
  min-width: 0;
  padding: 18px;
}

.mestek-card {
  grid-column: 1 / -1;
}

.source-header {
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
}

.source-heading {
  display: grid;
  gap: 8px;
}

.source-title h2 {
  font-size: var(--fs-title-sm);
}

.source-meta {
  color: var(--text-3);
  font-size: 12px;
  margin-bottom: 12px;
}

.frame-stats {
  color: var(--text-3);
  font-size: 12px;
  margin: 0 0 12px;
}

.source-errors {
  background: var(--warn-soft);
  border-radius: 8px;
  color: var(--warn-text);
  font-size: 12px;
  margin-bottom: 12px;
  padding: 10px 12px;
}

.source-errors p {
  margin: 0;
  overflow-wrap: anywhere;
}

.source-errors p + p {
  margin-top: 6px;
}

.sample-value {
  font-variant-numeric: tabular-nums;
}

.sample-value.raw {
  display: inline-block;
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: bottom;
  white-space: nowrap;
}

.source-empty {
  color: var(--text-3);
  margin: 18px 0 4px;
  text-align: center;
}

.trend-card {
  display: grid;
  gap: 16px;
  grid-template-columns: minmax(0, 1fr);
}

.trend-card :deep(.trend-wrap),
.trend-card :deep(.chart-box) {
  min-width: 0;
}

.trend-card :deep(.trend-wrap) {
  grid-template-columns: minmax(0, 1fr);
}

.trend-source {
  width: 250px;
}

.trend-metric {
  width: 210px;
}

@media (max-width: 1100px) {
  .page-header {
    display: grid;
  }

  .header-actions {
    justify-items: start;
  }

  .continuous-hint {
    text-align: left;
  }
}

@media (max-width: 900px) {
  .sources-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .mestek-card {
    grid-column: auto;
  }
}

@media (max-width: 768px) {
  .overview {
    align-items: flex-start;
    display: grid;
  }

  .monitor-actions {
    width: 100%;
  }

  .continuous-control {
    width: 100%;
  }

  .source-card,
  .trend-card {
    padding: 14px;
  }

  .trend-controls,
  .trend-source,
  .trend-metric {
    width: 100%;
  }
}
</style>
