<template>
  <DashboardPanel
    :title="t('dashboard.deviceStatus')"
    :icon="Odometer"
    :meta="t('dashboard.onlineCount', { online: onlineCount, total: instruments.length + 1 })"
    divided
  >
    <div class="card-list">
      <StateBlock
        :loading="loadingInstruments && !instrumentsData"
        :error="instrumentsError"
        :empty="!instruments.length"
        :error-text="t('dashboard.loadDevicesFailed')"
        :empty-text="t('dashboard.noDevices')"
        @retry="loadInstruments"
      >
        <div
          v-for="(inst, i) in instruments"
          :key="inst.id"
          class="dash-card device-card"
          :style="stagger(i)"
          @click="router.push('/instrument-measure')"
        >
          <span class="status-dot" :class="{ online: isOnline(inst.state) }"></span>
          <span class="device-name">{{ inst.name }}</span>
          <span class="device-state" :class="{ online: isOnline(inst.state) }">
            {{ isOnline(inst.state) ? t('common.online') : t('common.offline') }}
          </span>
          <el-icon class="card-chev"><ArrowRight /></el-icon>
        </div>
      </StateBlock>

      <div class="dash-card device-card gas-card" :style="stagger(instruments.length)" @click="router.push('/gas-control')">
        <div class="device-row">
          <span class="status-dot" :class="{ online: gasOnline }"></span>
          <span class="device-name">{{ t('dashboard.gasControl') }}</span>
          <span class="device-state" :class="{ online: gasOnline }">
            {{ gasOnline ? t('common.online') : t('common.offline') }}
          </span>
          <el-icon class="card-chev"><ArrowRight /></el-icon>
        </div>
        <div class="gas-stats" :class="{ offline: !gasOnline }">
          <div class="gas-stat">
            <span class="gas-label">{{ t('dashboard.runningState') }}</span>
            <span class="gas-value num">{{ gasRunningText }}</span>
          </div>
          <div class="gas-stat">
            <span class="gas-label">{{ t('dashboard.a1Pressure') }}</span>
            <span class="gas-value num">{{ gasA1Text }}</span>
          </div>
        </div>
      </div>
    </div>
  </DashboardPanel>
</template>

<script setup lang="ts">
// 首页设备状态块（结构改版 R6 §7.1 拆分）：DashboardView 设备 panel 等价平移，
// 仪器列表 + 气压控制卡，useAsyncData/StateBlock 逻辑原样迁移。
import { computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowRight, Odometer } from '@element-plus/icons-vue'
import { gasCellStatus, listInstruments, type GasCellPoint, type InstrumentSummary } from '@/api/instruments'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import StateBlock from '@/components/base/StateBlock.vue'
import DashboardPanel from '@/components/base/DashboardPanel.vue'

const router = useRouter()
const { t } = useI18n()

const RUNNING = 'GasCell:Piezo:Running'
const A1 = 'GasCell:Piezo:A1'

const {
  data: instrumentsData,
  loading: loadingInstruments,
  error: instrumentsError,
  run: loadInstruments
} = useAsyncData(() => listInstruments())
const instruments = computed(() => instrumentsData.value ?? [])
const gasData = reactive<Record<string, GasCellPoint>>({})

onMounted(() => {
  loadGasCell()
})

async function loadGasCell() {
  try {
    Object.assign(gasData, (await gasCellStatus()).data)
  } catch (err) {
    showApiError(err, t('dashboard.loadGasFailed'))
  }
}

function isOnline(state: string) {
  return state === 'running'
}

function point(pv: string): GasCellPoint {
  return gasData[pv] || { q: 'disconnected' }
}

// snapshot q !== 'good' 时视为离线（灰色展示）
const gasOnline = computed(() => point(RUNNING).q === 'good' && point(A1).q === 'good')

const gasRunningText = computed(() => {
  if (point(RUNNING).q !== 'good') return '—'
  return Number(point(RUNNING).v) ? t('dashboard.running') : t('dashboard.stopped')
})

const gasA1Text = computed(() => {
  const p = point(A1)
  if (p.q !== 'good' || p.v === undefined || p.v === null) return '—'
  const value = typeof p.v === 'number' ? Number(p.v.toPrecision(6)) : p.v
  return `${value} Pa`
})

// 设备列角标：在线数（仪器 + 气压控制）
const onlineCount = computed(
  () => instruments.value.filter((i) => isOnline(i.state)).length + (gasOnline.value ? 1 : 0)
)

// 卡片入场动画的交错延迟
function stagger(i: number) {
  return { animationDelay: `${i * 45}ms` }
}
</script>

<style scoped>
.card-list {
  align-content: start;
  display: grid;
  gap: var(--space-3);
  min-height: 80px;
}

.device-card {
  align-items: center;
  display: flex;
  gap: 10px;
}

.device-name {
  color: var(--text-1);
  font-weight: 600;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-dot {
  /* statusMeta onlineStatus：offline=--info 族 */
  background: var(--info);
  border-radius: 50%;
  flex-shrink: 0;
  height: 8px;
  position: relative;
  width: 8px;
}

.status-dot.online {
  /* statusMeta onlineStatus：online=--ok 族 */
  background: var(--ok);
}

.status-dot.online::after {
  animation: dot-pulse 2.2s ease-out infinite;
  border: 2px solid var(--ok);
  border-radius: 50%;
  content: '';
  inset: -4px;
  position: absolute;
}

@keyframes dot-pulse {
  0% {
    opacity: 0.8;
    transform: scale(0.6);
  }
  70%,
  100% {
    opacity: 0;
    transform: scale(1.15);
  }
}

.device-state {
  color: var(--text-3);
  flex-shrink: 0;
  font-size: 12px;
  margin-left: auto;
}

.device-state.online {
  color: var(--ok);
  font-weight: 600;
}

.card-chev {
  color: var(--text-3);
  flex-shrink: 0;
  font-size: 14px;
  transition:
    color 0.18s ease,
    translate 0.18s ease;
}

.device-card:hover .card-chev {
  color: var(--brand-600);
  translate: 2px 0;
}

/* 气压控制卡：淡品牌色底，与仪器卡区分 */

.gas-card {
  background: linear-gradient(150deg, var(--brand-050) 0%, var(--surface) 70%);
  border-color: var(--brand-100);
  display: block;
}

.device-row {
  align-items: center;
  display: flex;
  gap: 10px;
}

.gas-stats {
  display: grid;
  gap: 10px;
  grid-template-columns: 1fr 1fr;
  margin-top: 12px;
}

.gas-stat {
  background: var(--surface-2);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 1px;
  padding: 8px 12px;
}

.gas-label {
  color: var(--text-3);
  font-size: 12px;
}

.gas-value {
  color: var(--text-1);
  font-size: 14px;
  font-weight: var(--fw-semibold);
}

.gas-stats.offline .gas-value {
  color: var(--text-3);
}

@media (max-width: 768px) {
  .gas-stats {
    grid-template-columns: 1fr;
  }
}
</style>