<template>
  <div class="metric-panel">
    <span class="metric-label">{{ label }}</span>
    <div class="sparkline" :style="{ color }">
      <svg v-if="points.length" viewBox="0 0 300 72" preserveAspectRatio="none" role="img" :aria-label="trendLabel">
        <title>{{ trendLabel }}</title>
        <line class="grid-line" x1="0" y1="18" x2="300" y2="18" />
        <line class="grid-line" x1="0" y1="54" x2="300" y2="54" />
        <path class="trend-area" :d="paths.area" />
        <path class="trend-line" :d="paths.line" />
        <circle class="trend-end" :cx="paths.endX" :cy="paths.endY" r="3" />
      </svg>
      <span v-else class="trend-empty">{{ emptyText }}</span>
    </div>
    <div class="metric-reading" :class="{ missing: value === undefined }">
      <strong>{{ formattedValue }}</strong>
      <span v-if="value !== undefined && unit">{{ unit }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ChartPoint } from '@/utils/chartTheme'
import { buildSparklinePaths } from '@/utils/electricalTrend'

const props = defineProps<{
  label: string
  value?: number
  unit?: string
  points: ChartPoint[]
  color: string
  emptyText: string
  trendLabel: string
}>()

const paths = computed(() => buildSparklinePaths(props.points))
const formattedValue = computed(() => {
  const value = props.value
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  if (value === 0) return '0'
  const absolute = Math.abs(value)
  if (absolute >= 1e6 || absolute < 1e-3) return value.toExponential(3)
  return String(Number(value.toPrecision(6)))
})
</script>

<style scoped>
.metric-panel {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.metric-label {
  color: var(--text-3);
  font-size: var(--fs-caption);
}

.sparkline {
  align-items: center;
  background: var(--surface-2);
  border-radius: var(--radius-sm);
  display: flex;
  height: 58px;
  justify-content: center;
  overflow: hidden;
}

.sparkline svg {
  display: block;
  height: 58px;
  overflow: visible;
  width: 100%;
}

.grid-line {
  stroke: var(--border);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}

.trend-area {
  fill: currentColor;
  opacity: .08;
}

.trend-line {
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.8;
  vector-effect: non-scaling-stroke;
}

.trend-end {
  fill: currentColor;
  stroke: var(--surface-2);
  stroke-width: 1.5;
  vector-effect: non-scaling-stroke;
}

.trend-empty {
  color: var(--text-3);
  font-size: var(--fs-caption);
}

.metric-reading {
  align-items: baseline;
  color: var(--text-1);
  display: flex;
  flex-wrap: wrap;
  font-variant-numeric: tabular-nums;
  gap: 5px;
  min-height: 28px;
}

.metric-reading strong {
  font-size: var(--fs-title-lg);
  font-weight: var(--fw-semibold);
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.metric-reading span {
  color: var(--text-3);
  font-size: var(--fs-caption);
}

.metric-reading.missing strong {
  color: var(--text-3);
}
</style>
