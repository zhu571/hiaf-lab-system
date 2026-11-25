<template>
  <div class="curve-wrap">
    <div class="curve-head">
      <span>{{ t('scan.curve') }}</span>
      <span class="curve-hint">{{ t('scan.curveHint') }}</span>
    </div>
    <div v-if="loading" v-loading="true" class="curve-loading" />
    <el-empty v-else-if="error" :description="t('scan.curveLoadFailed')" />
    <canvas v-else ref="canvas" class="curve-canvas" />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart } from 'chart.js'
import { api } from '@/api/client'
import { useTheme } from '@/composables/useTheme'
import { chartPalette, refreshDefaults, LINE_DASHES } from '@/utils/chartTheme'

const props = defineProps<{ scanId: string; columns: string[] }>()
const { t } = useI18n()
const theme = useTheme()
const canvas = ref<HTMLCanvasElement>()
const loading = ref(false)
const error = ref(false)

let chart: Chart<'line'> | undefined

const MAX_ROWS = 2000

function parseRows(text: string): { labels: string[]; series: { name: string; values: (number | null)[] }[] } {
  const lines = text.replace(/\r/g, '').split('\n').filter((line) => line.trim() !== '')
  if (!lines.length) return { labels: [], series: [] }
  const start = /^[0-9.\-+eE\t]+$/.test(lines[0].split('\t')[0] ?? '') ? 0 : 1
  const rows = lines.slice(start)
  if (!rows.length) return { labels: [], series: [] }
  const step = Math.max(1, Math.ceil(rows.length / MAX_ROWS))
  const sampled = rows.filter((_, index) => index % step === 0)
  const columns = props.columns.length ? props.columns : sampled[0].split('\t').map((_, i) => `col${i + 1}`)
  const series = columns.slice(1, 6).map((name, index) => ({
    name,
    values: sampled.map((row) => {
      const cell = row.split('\t')[index + 1]
      const value = Number(cell)
      return cell === '' || Number.isNaN(value) ? null : value
    }),
  }))
  const labels = sampled.map((row) => {
    const value = Number(row.split('\t')[0])
    return Number.isFinite(value) ? String(Number(value.toPrecision(4))) : ''
  })
  return { labels, series: series.filter((s) => s.values.some((v) => v !== null)) }
}

function draw(text: string) {
  if (!canvas.value) return
  const { labels, series } = parseRows(text)
  if (!labels.length || !series.length) {
    error.value = true
    return
  }
  refreshDefaults()
  const palette = chartPalette()
  chart?.destroy()
  chart = new Chart(canvas.value, {
    type: 'line',
    data: {
      labels,
      datasets: series.map((s, index) => ({
        label: s.name,
        data: s.values,
        borderColor: palette[index % palette.length],
        backgroundColor: palette[index % palette.length],
        borderDash: LINE_DASHES[index % LINE_DASHES.length],
        borderWidth: 2,
        pointRadius: 0,
        tension: 0.25,
        spanGaps: true,
      })),
    },
    options: {
      animation: false,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: { position: 'bottom', labels: { usePointStyle: true, boxWidth: 8, boxHeight: 8 } },
        tooltip: { callbacks: { label: (item) => `${item.dataset.label || ''}: ${Number(item.parsed.y).toPrecision(5)}` } },
      },
      scales: {
        x: { ticks: { maxTicksLimit: 10, maxRotation: 0, autoSkip: true } },
        y: { type: 'linear' },
      },
    },
  })
}

async function load() {
  if (!props.scanId) return
  loading.value = true
  error.value = false
  try {
    const response = await api.get(`/scans/${props.scanId}/file/txt`, { responseType: 'text' })
    draw(String(response.data))
  } catch {
    error.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => [props.scanId, props.columns.join(',')], load)
watch(theme.state, load)
onBeforeUnmount(() => chart?.destroy())
</script>

<style scoped>
.curve-wrap {
  background: var(--surface-2);
  border-radius: var(--radius-md);
  padding: var(--space-3);
}

.curve-head {
  display: flex;
  justify-content: space-between;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
  font-size: var(--fs-body-sm);
  color: var(--text-2);
}

.curve-hint {
  color: var(--text-3);
}

.curve-loading,
.curve-canvas {
  height: 260px;
  width: 100%;
}
</style>
