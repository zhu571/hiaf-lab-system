<template>
  <div class="electrical-page">
    <header class="page-header">
      <div>
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
        <section class="overview" aria-label="Acquisition overview">
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
        <el-alert v-if="allSamples.length === 0" :title="t('electrical.empty')" type="info" :closable="false" show-icon />
        <el-alert v-if="historyError" :title="t('electrical.historyFailed')" type="warning" :closable="false" show-icon>
          <el-button size="small" @click="loadHistory">{{ t('electrical.retry') }}</el-button>
        </el-alert>

        <div class="primary-grid">
          <section class="source-card mestek-card" data-source="mestek">
            <header class="source-header">
              <div class="source-heading">
                <div class="source-title">
                  <h2>{{ sourceLabel('mestek') }}</h2>
                  <el-tag size="small" effect="plain">{{ modeLabel(healthFor('mestek')?.mode || 'modbus_active') }}</el-tag>
                </div>
                <div class="source-meta">
                  <span>{{ t('electrical.channelCoverage', { read: readCount('mestek', MESTEK_CHANNELS, 'U_V'), total: 13 }) }}</span>
                  <span v-if="healthFor('mestek')?.last_sample_at">{{ t('electrical.lastSample', { time: formatDateTime(healthFor('mestek')!.last_sample_at!) }) }}</span>
                </div>
              </div>
              <StatusBadge domain="electricalHealth" :value="healthFor('mestek')?.status || 'connecting'" />
            </header>
            <el-alert
              v-for="message in healthAlerts('mestek')"
              :key="message"
              class="source-alert"
              :title="message"
              type="warning"
              :closable="false"
              show-icon
            />

            <table class="mestek-table" :aria-label="sourceLabel('mestek')">
              <thead>
                <tr>
                  <th scope="col">{{ t('electrical.channel') }}</th>
                  <th scope="col">{{ metricLabel('setU_V') }} <span class="column-unit">V</span></th>
                  <th scope="col">{{ metricLabel('setI_A') }} <span class="column-unit">A</span></th>
                  <th scope="col">{{ metricLabel('U_V') }} <span class="column-unit">V</span></th>
                  <th scope="col">{{ metricLabel('I_A') }} <span class="column-unit">A</span></th>
                  <th scope="col">{{ t('electrical.arcStatus') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="channel in MESTEK_CHANNELS" :key="channel" class="mestek-row" :data-channel="channel">
                  <th scope="row">{{ channel }}</th>
                  <td
                    v-for="metric in MESTEK_METRICS"
                    :key="metric"
                    class="reading-cell"
                    :class="{ missing: numericValue('mestek', channel, metric) === undefined }"
                    :data-metric="metric"
                    :data-label="metricLabel(metric)"
                  >{{ formatReading(numericValue('mestek', channel, metric)) }}</td>
                  <td class="arc-cell" :data-label="t('electrical.arcStatus')">
                    <div
                      class="arc-status"
                      :class="arcState(channel)"
                      :title="t('electrical.arcThreshold')"
                      :data-state="arcState(channel)"
                    >
                      <span class="arc-dot" aria-hidden="true"></span>
                      <span>{{ arcLabel(channel) }}</span>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
            <p class="card-note">{{ t('electrical.setCurrentNote') }}</p>
            <p v-if="unmappedCount" class="card-note">{{ t('electrical.unmappedHidden', { count: unmappedCount }) }}</p>
          </section>

          <section class="source-card generator-card" data-card="generators">
            <header class="source-header">
              <div class="source-heading">
                <div class="source-title">
                  <h2>{{ t('electrical.generatorTitle') }}</h2>
                  <el-tag size="small" effect="plain">{{ t('electrical.scpiReadOnly') }}</el-tag>
                </div>
                <div class="source-meta">
                  <span>{{ t('electrical.channelCoverage', { read: generatorReadCount, total: 3 }) }}</span>
                </div>
              </div>
              <StatusBadge domain="electricalHealth" :value="sourceStatus(...GENERATOR_SOURCES)" />
            </header>
            <el-alert
              v-for="message in healthAlerts(...GENERATOR_SOURCES)"
              :key="message"
              class="source-alert"
              :title="message"
              type="warning"
              :closable="false"
              show-icon
            />
            <div class="generator-list">
              <article
                v-for="row in GENERATOR_ROWS"
                :key="`${row.source}-${row.channel}`"
                class="generator-row"
                :data-source="row.source"
                :data-channel="row.channel"
              >
                <header class="generator-name">
                  <strong>{{ sourceLabel(row.source) }}</strong>
                  <span>{{ row.channel }}</span>
                </header>
                <div class="generator-values">
                  <div v-for="metric in GENERATOR_METRICS" :key="metric" class="generator-reading" :data-metric="metric">
                    <span>{{ metricLabel(metric) }}</span>
                    <div class="numeric-reading" :class="{ missing: numericValue(row.source, row.channel, metric) === undefined }">
                      <strong>{{ formatReading(numericValue(row.source, row.channel, metric)) }}</strong>
                      <span v-if="numericValue(row.source, row.channel, metric) !== undefined">{{ sampleFor(row.source, row.channel, metric)?.unit }}</span>
                    </div>
                  </div>
                </div>
              </article>
            </div>
          </section>

          <section class="source-card scopes-card" data-card="scopes">
            <header class="source-header">
              <div class="source-heading">
                <div class="source-title">
                  <h2>{{ t('electrical.scopesTitle') }}</h2>
                  <el-tag size="small" effect="plain">{{ t('electrical.scpiReadOnly') }}</el-tag>
                </div>
                <div class="source-meta">
                  <span>{{ t('electrical.measurementCount', { count: scopeReadCount }) }}</span>
                </div>
              </div>
              <StatusBadge domain="electricalHealth" :value="sourceStatus(...SCOPE_SOURCES)" />
            </header>
            <el-alert
              v-for="message in healthAlerts(...SCOPE_SOURCES)"
              :key="message"
              class="source-alert"
              :title="message"
              type="warning"
              :closable="false"
              show-icon
            />
            <div class="scope-list">
              <article
                v-for="row in SCOPE_ROWS"
                :key="`${row.source}-${row.channel}`"
                class="scope-reading"
                :data-source="row.source"
                :data-channel="row.channel"
                :data-metric="row.metric"
              >
                <div class="scope-identity">
                  <strong>{{ scopeBrand(row.source) }}</strong>
                  <span>{{ row.channel }}</span>
                </div>
                <span class="measurement-kind">{{ metricLabel(row.metric) }}</span>
                <div class="numeric-reading scope-value" :class="{ missing: numericValue(row.source, row.channel, row.metric) === undefined }">
                  <strong>{{ formatReading(numericValue(row.source, row.channel, row.metric)) }}</strong>
                  <span v-if="numericValue(row.source, row.channel, row.metric) !== undefined">{{ sampleFor(row.source, row.channel, row.metric)?.unit }}</span>
                </div>
              </article>
            </div>
            <p class="card-note">{{ t('electrical.scopesNote') }}</p>
          </section>
        </div>

        <section class="source-card keithley6485-card" data-source="keithley6485">
          <header class="source-header">
            <div class="source-heading">
              <div class="source-title">
                <h2>{{ sourceLabel('keithley6485') }}</h2>
                <el-tag size="small" effect="plain">{{ modeLabel(healthFor('keithley6485')?.mode || 'passive') }}</el-tag>
              </div>
              <div class="source-meta">
                <span v-if="healthFor('keithley6485')?.last_sample_at">{{ t('electrical.lastSample', { time: formatDateTime(healthFor('keithley6485')!.last_sample_at!) }) }}</span>
              </div>
            </div>
            <StatusBadge domain="electricalHealth" :value="healthFor('keithley6485')?.status || 'connecting'" />
          </header>
          <el-alert
            v-for="message in healthAlerts('keithley6485')"
            :key="message"
            class="source-alert"
            :title="message"
            type="warning"
            :closable="false"
            show-icon
          />
          <ElectricalMetricPanel
            class="pico-current"
            data-channel="CH1"
            data-metric="current_A"
            :label="metricLabel('current_A')"
            :value="numericValue('keithley6485', 'CH1', 'current_A')"
            :unit="sampleFor('keithley6485', 'CH1', 'current_A')?.unit"
            :points="trendPoints"
            :color="picoColor"
            :empty-text="t('electrical.noTrendPoints')"
            :trend-label="t('electrical.trendLabel', { channel: 'CH1', metric: metricLabel('current_A') })"
          />
          <p class="trend-footnote">{{ t('electrical.trendHint') }}</p>
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
  getElectricalHistory,
  getElectricalLatest,
  singleShotElectrical,
  type ElectricalLatest,
  type ElectricalSample,
  type ElectricalSingleShotResult,
  type ElectricalSourceHealth
} from '../api/electrical'
import { isApiError } from '../api/client'
import { usePolling } from '../composables/usePolling'
import { showApiError } from '../composables/useNotify'
import { useTheme } from '../composables/useTheme'
import ElectricalMetricPanel from '@/components/business/ElectricalMetricPanel.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { chartPalette, type ChartPoint } from '@/utils/chartTheme'
import { formatDateTime } from '@/utils/datetime'
import {
  electricalSampleKey,
  electricalSeriesKey,
  mergeElectricalTrend,
  mestekArcState,
  type MestekArcState
} from '@/utils/electricalTrend'

const { t } = useI18n()

function channelRange(count: number) {
  return Array.from({ length: count }, (_, index) => `CH${index + 1}`)
}

const MESTEK_CHANNELS = channelRange(13)
const MESTEK_METRICS = ['setU_V', 'setI_A', 'U_V', 'I_A']
const GENERATOR_SOURCES = ['agilent_33512b', 'agilent_33210a']
const GENERATOR_ROWS = [
  { source: 'agilent_33512b', channel: 'CH1' },
  { source: 'agilent_33512b', channel: 'CH2' },
  { source: 'agilent_33210a', channel: 'CH1' }
]
const GENERATOR_METRICS = ['amplitude_V', 'frequency_Hz']
const SCOPE_SOURCES = ['rigol_dho814', 'tek_mdo3034']
const SCOPE_ROWS = [
  ...channelRange(4).map((channel) => ({ source: 'rigol_dho814', channel, metric: 'vpp_V' })),
  ...channelRange(2).map((channel) => ({ source: 'tek_mdo3034', channel, metric: 'measurement_value' }))
]
const SOURCE_LABEL_KEYS: Record<string, string> = {
  mestek: 'electrical.sources.mestek',
  keithley6485: 'electrical.sources.keithley6485',
  rigol_dho814: 'electrical.sources.rigol_dho814',
  tek_mdo3034: 'electrical.sources.tek_mdo3034',
  agilent_33512b: 'electrical.sources.agilent_33512b',
  agilent_33210a: 'electrical.sources.agilent_33210a'
}
const METRIC_LABEL_KEYS: Record<string, string> = {
  I_A: 'electrical.metrics.I_A',
  U_V: 'electrical.metrics.U_V',
  setU_V: 'electrical.metrics.setU_V',
  setI_A: 'electrical.metrics.setI_A',
  current_A: 'electrical.metrics.current_A',
  vpp_V: 'electrical.metrics.vpp_V',
  measurement_value: 'electrical.metrics.measurement_value',
  frequency_Hz: 'electrical.metrics.frequency_Hz',
  amplitude_V: 'electrical.metrics.amplitude_V'
}

const latest = ref<ElectricalLatest>()
const continuousView = ref(true)
const loading = ref(true)
const refreshing = ref(false)
const shotRunning = ref(false)
const historyLoading = ref(false)
const loadError = ref<Error | null>(null)
const historyError = ref(false)
const trendHistory = ref<Record<string, ChartPoint[]>>({})

const allSamples = computed(() => latest.value?.samples ?? [])
const totalWriteErrors = computed(() => (latest.value?.sources ?? []).reduce((total, source) => total + source.write_errors, 0))
const errorMessage = computed(() => loadError.value ? `${t('electrical.loadFailed')}: ${loadError.value.message}` : '')
const sampleLookup = computed(() => {
  const result = new Map<string, ElectricalSample>()
  for (const sample of allSamples.value) {
    if (sample.channel === 'UNMAPPED') continue
    const key = electricalSeriesKey(sample.source, sample.channel, sample.metric)
    const previous = result.get(key)
    if (!previous || Date.parse(sample.time) >= Date.parse(previous.time)) result.set(key, sample)
  }
  return result
})
const generatorReadCount = computed(() => GENERATOR_ROWS.filter((row) => numericValue(row.source, row.channel, 'amplitude_V') !== undefined).length)
const scopeReadCount = computed(() => SCOPE_ROWS.filter((row) => numericValue(row.source, row.channel, row.metric) !== undefined).length)
const unmappedCount = computed(() => allSamples.value.filter((sample) => sample.source === 'mestek' && sample.channel === 'UNMAPPED').length)
const trendPoints = computed(() => trendHistory.value[electricalSeriesKey('keithley6485', 'CH1', 'current_A')] ?? [])

const { state: themeState } = useTheme()
const picoColor = computed(() => {
  void themeState.value
  return chartPalette()[0]
})

function sampleFor(source: string, channel: string, metric: string) {
  return sampleLookup.value.get(electricalSeriesKey(source, channel, metric))
}

function numericValue(source: string, channel: string, metric: string) {
  const value = sampleFor(source, channel, metric)?.value
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function formatReading(value: number | undefined) {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  if (value === 0) return '0'
  const absolute = Math.abs(value)
  if (absolute >= 1e6 || absolute < 1e-3) return value.toExponential(3)
  return String(Number(value.toPrecision(6)))
}

function readCount(source: string, channels: string[], metric: string) {
  return channels.filter((channel) => numericValue(source, channel, metric) !== undefined).length
}

function healthFor(source: string) {
  return latest.value?.sources?.find((item) => item.source === source)
}

function sourceStatus(...sources: string[]): ElectricalSourceHealth['status'] {
  const health = sources.map(healthFor)
  if (health.some((item) => item?.status === 'degraded' || item?.connected === false)) return 'degraded'
  if (health.some((item) => item?.status === 'disabled')) return 'disabled'
  return health.every((item) => item?.status === 'ok') ? 'ok' : 'connecting'
}

function healthAlerts(...sources: string[]) {
  return Array.from(new Set(sources.flatMap((source) => {
    const health = healthFor(source)
    return [health?.last_error, health?.storage_error].filter((message): message is string => Boolean(message))
  })))
}

function sourceLabel(source: string) {
  const key = SOURCE_LABEL_KEYS[source]
  return key ? t(key) : source
}

function scopeBrand(source: string) {
  return source === 'rigol_dho814' ? 'RIGOL' : 'Tek'
}

function modeLabel(mode: ElectricalSourceHealth['mode']) {
  const labelKeys = {
    passive: 'electrical.passiveReadOnly',
    modbus_active: 'electrical.activeReadOnly',
    scpi: 'electrical.scpiReadOnly'
  } as const
  return t(labelKeys[mode])
}

function metricLabel(metric: string) {
  const key = METRIC_LABEL_KEYS[metric]
  return key ? t(key) : metric
}

function recordTrend(samples: ElectricalSample[]) {
  trendHistory.value = mergeElectricalTrend(
    trendHistory.value,
    samples.filter((sample) => sample.source === 'keithley6485' && sample.channel === 'CH1' && sample.metric === 'current_A')
  )
}

function arcState(channel: string): MestekArcState {
  return mestekArcState(numericValue('mestek', channel, 'I_A'))
}

function arcLabel(channel: string) {
  const state = arcState(channel)
  if (state === 'danger') return t('electrical.arcHigh')
  if (state === 'unknown') return t('electrical.arcUnknown')
  return numericValue('mestek', channel, 'I_A') === 0 ? t('electrical.arcZero') : t('electrical.arcSafe')
}

const polling = usePolling(refreshLatest, 2000)
const historyPolling = usePolling(loadHistory, 60000)
onMounted(() => {
  void refreshLatest()
  void loadHistory()
  if (continuousView.value) {
    polling.start()
    historyPolling.start()
  }
})
watch(continuousView, (enabled) => {
  if (enabled) {
    void refreshLatest()
    void loadHistory()
    polling.start()
    historyPolling.start()
  } else {
    polling.stop()
    historyPolling.stop()
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

async function loadHistory() {
  if (historyLoading.value) return
  historyLoading.value = true
  try {
    const result = await getElectricalHistory({
      source: 'keithley6485',
      channel: 'CH1',
      metric: 'current_A',
      from: '-30m',
      limit: 1000
    })
    recordTrend(result.points ?? [])
    historyError.value = false
  } catch {
    historyError.value = true
  } finally {
    historyLoading.value = false
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
  const activeMESTEK = result.sources?.some((source) => source.source === 'mestek' && source.mode === 'modbus_active')
  const retained = activeMESTEK ? allSamples.value.filter((sample) => sample.source !== 'mestek') : allSamples.value
  const samples = new Map(retained.map((sample) => [electricalSampleKey(sample), sample]))
  for (const sample of result.samples ?? []) {
    samples.set(electricalSampleKey(sample), sample)
  }
  latest.value = {
    continuous_enabled: latest.value?.continuous_enabled ?? false,
    updated_at: result.finished_at,
    write_backlog: latest.value?.write_backlog ?? 0,
    sources: result.sources ?? latest.value?.sources ?? [],
    samples: Array.from(samples.values())
  }
}
</script>

<style scoped>
.electrical-page,
.monitor-content {
  display: grid;
  gap: var(--space-5);
}

.page-header,
.overview,
.source-header,
.monitor-actions,
.continuous-control,
.overview-status,
.storage-status {
  align-items: center;
  display: flex;
}

.page-header,
.overview,
.source-header {
  justify-content: space-between;
}

.page-header {
  align-items: flex-start;
  gap: var(--space-5);
}

.page-header h1,
.source-title h2 {
  margin: 0;
}

.page-header h1 {
  font-size: var(--fs-title-xl);
}

.subtitle,
.continuous-hint,
.card-note,
.trend-footnote {
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
.source-title {
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
  font-size: var(--fs-caption);
  max-width: 540px;
  text-align: right;
}

.overview {
  border-bottom: 1px solid var(--border);
  border-top: 1px solid var(--border);
  gap: var(--space-3);
  padding: 12px 2px;
}

.overview-status {
  color: var(--text-3);
}

.primary-grid {
  align-items: start;
  display: grid;
  gap: var(--space-4);
  grid-template-columns: minmax(0, 1.7fr) minmax(0, 1fr) minmax(0, .85fr);
}

.source-card,
.source-heading {
  min-width: 0;
}

.source-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  padding: var(--space-5);
}

.source-header {
  align-items: flex-start;
  gap: var(--space-3);
}

.source-title {
  align-items: center;
  display: flex;
}

.source-title h2 {
  font-size: var(--fs-title);
}

.source-meta {
  align-items: center;
  color: var(--text-3);
  display: flex;
  flex-wrap: wrap;
  font-size: var(--fs-caption);
  gap: 6px 16px;
  margin-top: 5px;
}

.source-alert {
  margin-top: var(--space-3);
}

.mestek-table {
  border-collapse: collapse;
  font-size: var(--fs-caption);
  margin-top: var(--space-3);
  table-layout: fixed;
  width: 100%;
}

.mestek-table th,
.mestek-table td {
  border-bottom: 1px solid var(--border);
  font-variant-numeric: tabular-nums;
  padding: 9px 4px;
  text-align: right;
  white-space: nowrap;
}

.mestek-table thead th {
  color: var(--text-3);
  font-size: var(--fs-caption);
  font-weight: var(--fw-medium);
  line-height: 1.35;
  white-space: normal;
}

.mestek-table th:first-child {
  text-align: left;
  width: 13%;
}

.mestek-table th:last-child {
  text-align: center;
  width: 22%;
}

.mestek-table tbody th {
  font-weight: var(--fw-semibold);
}

.mestek-table tbody tr:last-child th,
.mestek-table tbody tr:last-child td {
  border-bottom: 0;
}

.column-unit {
  color: var(--text-3);
  font-size: var(--fs-caption);
  font-weight: var(--fw-regular);
}

.reading-cell.missing,
.numeric-reading.missing strong {
  color: var(--text-3);
}

.arc-status {
  align-items: center;
  border-radius: var(--radius-full);
  display: inline-flex;
  font-size: var(--fs-caption);
  font-weight: var(--fw-semibold);
  gap: 6px;
  min-height: 24px;
  padding: 2px 8px;
  white-space: nowrap;
}

.arc-dot {
  background: currentColor;
  border-radius: 50%;
  flex: 0 0 auto;
  height: 7px;
  width: 7px;
}

.arc-status.danger {
  background: var(--danger-soft);
  color: var(--danger-text);
}

.arc-status.safe {
  background: var(--ok-soft);
  color: var(--ok-text);
}

.arc-status.unknown {
  background: var(--info-soft);
  color: var(--info-text);
}

.card-note,
.trend-footnote {
  font-size: var(--fs-caption);
  line-height: 1.5;
}

.card-note {
  margin-top: var(--space-3);
}

.generator-list,
.scope-list {
  margin-top: var(--space-2);
}

.generator-row,
.scope-reading {
  border-bottom: 1px solid var(--border);
}

.generator-row:last-child,
.scope-reading:last-child {
  border-bottom: 0;
}

.generator-row {
  padding: var(--space-4) 0;
}

.generator-name {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  font-size: var(--fs-caption);
  gap: 8px;
  justify-content: space-between;
}

.generator-name span,
.scope-identity span,
.measurement-kind {
  color: var(--text-3);
}

.generator-values {
  display: grid;
  gap: var(--space-4);
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin-top: var(--space-3);
}

.generator-reading {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.generator-reading > span,
.measurement-kind {
  font-size: var(--fs-caption);
}

.numeric-reading {
  align-items: baseline;
  display: flex;
  flex-wrap: wrap;
  font-variant-numeric: tabular-nums;
  gap: 5px;
  min-width: 0;
}

.numeric-reading strong {
  font-size: var(--fs-title);
  font-weight: var(--fw-semibold);
  overflow-wrap: anywhere;
}

.numeric-reading span {
  color: var(--text-3);
  font-size: var(--fs-caption);
}

.scope-reading {
  align-items: center;
  display: grid;
  gap: 4px var(--space-3);
  grid-template-columns: minmax(0, 1fr) auto;
  padding: 13px 0;
}

.scope-identity {
  align-items: baseline;
  display: flex;
  flex-wrap: wrap;
  font-size: var(--fs-caption);
  gap: 7px;
}

.scope-value {
  grid-column: 2;
  grid-row: 1 / span 2;
  justify-content: flex-end;
}

.pico-current {
  margin-top: var(--space-4);
}

.pico-current :deep(.sparkline),
.pico-current :deep(.sparkline svg) {
  height: 220px;
}

.pico-current :deep(.metric-reading) {
  margin-top: var(--space-2);
}

.pico-current :deep(.metric-reading strong) {
  font-size: var(--fs-metric);
}

.trend-footnote {
  text-align: right;
}

@media (max-width: 1200px) {
  .primary-grid {
    grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  }

  .mestek-card {
    grid-row: span 2;
  }
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
  .primary-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .mestek-card {
    grid-row: auto;
  }
}

@media (max-width: 768px) {
  .overview {
    align-items: flex-start;
    display: grid;
  }

  .monitor-actions,
  .continuous-control {
    width: 100%;
  }

  .source-card {
    padding: var(--space-4);
  }

  .trend-footnote {
    text-align: left;
  }
}

@media (max-width: 600px) {
  .mestek-table,
  .mestek-table tbody {
    display: block;
  }

  .mestek-table thead {
    display: none;
  }

  .mestek-table .mestek-row {
    border-bottom: 1px solid var(--border);
    display: grid;
    gap: 8px var(--space-3);
    grid-template-columns: repeat(2, minmax(0, 1fr));
    padding: var(--space-3) 0;
  }

  .mestek-table .mestek-row th,
  .mestek-table .mestek-row td {
    border: 0;
    padding: 0;
    white-space: normal;
  }

  .mestek-table .mestek-row th {
    grid-column: 1 / -1;
  }

  .mestek-table .mestek-row td {
    align-items: baseline;
    display: flex;
    gap: 8px;
    justify-content: space-between;
    text-align: right;
  }

  .mestek-table .mestek-row td::before {
    color: var(--text-3);
    content: attr(data-label);
    font-size: var(--fs-caption);
    text-align: left;
  }

  .mestek-table .mestek-row .arc-cell {
    align-items: center;
    grid-column: 1 / -1;
  }
}
</style>
