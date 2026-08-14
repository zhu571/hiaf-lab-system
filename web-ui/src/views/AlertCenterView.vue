<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('alert.title') }}</h2>
    </div>
    <el-tabs v-model="tab" class="alert-tabs" @tab-change="onTabChange">
      <el-tab-pane :label="t('alert.tabActive')" name="active">
        <section class="panel">
          <ResponsiveTable :rows="rows" :loading="loading">
            <el-table-column :label="t('alert.level')" width="100">
              <template #default="{ row }">
                <el-tag :type="levelTagType(row.level)" size="small">{{ levelLabel(row.level) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('alert.source')" width="110">
              <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
            </el-table-column>
            <el-table-column :label="t('alert.colTitle')">
              <template #default="{ row }">
                <el-tooltip :content="row.detail" placement="top-start" :show-after="300" :disabled="!row.detail">
                  <span class="title-cell">{{ row.title }}</span>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column :label="t('alert.occurrence')" width="90" align="center">
              <template #default="{ row }">
                <el-badge :value="row.occurrence_count" :max="999" type="warning" />
              </template>
            </el-table-column>
            <el-table-column prop="first_seen" :label="t('alert.firstSeen')" width="170" show-overflow-tooltip />
            <el-table-column prop="last_seen" :label="t('alert.lastSeen')" width="170" show-overflow-tooltip />
            <el-table-column v-if="canResolve" :label="t('alert.actions')" width="110" align="center">
              <template #default="{ row }">
                <el-button link type="primary" size="small" :loading="resolvingId === row.id" @click="onResolve(row)">
                  {{ t('alert.resolve') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('alert.emptyActive')" />
            </template>
            <template #card="{ row }">
              <div class="alert-card">
                <span class="card-title">
                  <el-tag :type="levelTagType(row.level)" size="small">{{ levelLabel(row.level) }}</el-tag>
                  <span class="card-source">{{ sourceLabel(row.source) }}</span>
                  {{ row.title }}
                </span>
                <div class="card-fields">
                  <span>{{ t('alert.occurrence') }} {{ row.occurrence_count }}</span>
                  <span>{{ row.first_seen }}</span>
                  <span>{{ row.last_seen }}</span>
                </div>
                <el-button v-if="canResolve" link type="primary" size="small" :loading="resolvingId === row.id" @click="onResolve(row)">
                  {{ t('alert.resolve') }}
                </el-button>
              </div>
            </template>
          </ResponsiveTable>
        </section>
      </el-tab-pane>
      <el-tab-pane :label="t('alert.tabHistory')" name="resolved">
        <section class="panel">
          <ResponsiveTable :rows="rows" :loading="loading">
            <el-table-column :label="t('alert.level')" width="100">
              <template #default="{ row }">
                <el-tag :type="levelTagType(row.level)" size="small">{{ levelLabel(row.level) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('alert.source')" width="110">
              <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
            </el-table-column>
            <el-table-column prop="title" :label="t('alert.colTitle')" show-overflow-tooltip />
            <el-table-column :label="t('alert.occurrence')" width="90" align="center">
              <template #default="{ row }">
                <el-badge :value="row.occurrence_count" :max="999" type="warning" />
              </template>
            </el-table-column>
            <el-table-column prop="last_seen" :label="t('alert.lastSeen')" width="170" show-overflow-tooltip />
            <el-table-column :label="t('alert.resolved')" width="170" show-overflow-tooltip>
              <template #default="{ row }">
                <span v-if="row.resolved_at">{{ row.resolved_at }}</span>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column prop="resolved_by" :label="t('alert.resolvedBy')" width="120" />
            <template #empty>
              <el-empty :description="t('alert.emptyHistory')" />
            </template>
            <template #card="{ row }">
              <div class="alert-card">
                <span class="card-title">
                  <el-tag :type="levelTagType(row.level)" size="small">{{ levelLabel(row.level) }}</el-tag>
                  <span class="card-source">{{ sourceLabel(row.source) }}</span>
                  {{ row.title }}
                </span>
                <div class="card-fields">
                  <span>{{ t('alert.occurrence') }} {{ row.occurrence_count }}</span>
                  <span>{{ row.last_seen }}</span>
                  <span>{{ t('alert.resolvedBy') }}: {{ row.resolved_by || '-' }}</span>
                </div>
              </div>
            </template>
          </ResponsiveTable>
          <el-pagination
            v-model:current-page="page"
            class="pager"
            layout="total, prev, pager, next"
            :page-size="HISTORY_PAGE_SIZE"
            :total="total"
            @current-change="load"
          />
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { listAlerts, resolveAlert, type AlertLevel, type AlertRecord } from '../api/alerts'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

// resolve 仅 admin/maintainer（后端 RequireRoleOrService 强校验，按钮隐藏只是 UX）
const canResolve = computed(() => auth.isAdmin || auth.user?.role === 'maintainer')

const tab = ref<'active' | 'resolved'>('active')
const rows = ref<AlertRecord[]>([])
const loading = ref(false)
const resolvingId = ref('')
const page = ref(1)
const total = ref(0)
const loadSeq = ref(0)

// 历史 tab 仅展示 90 天内（后端滚动清理），每页 50 条 + offset 简单分页
const HISTORY_PAGE_SIZE = 50

onMounted(load)

function onTabChange() {
  page.value = 1
  load()
}

async function load() {
  // 递增序号：切 tab/翻页竞态时丢弃过期响应，避免旧 tab 数据覆盖新 tab
  const seq = ++loadSeq.value
  loading.value = true
  try {
    const status = tab.value
    const params: Record<string, string | number> = { status }
    if (status === 'resolved') {
      params.limit = HISTORY_PAGE_SIZE
      params.offset = (page.value - 1) * HISTORY_PAGE_SIZE
    } else {
      params.limit = 100
    }
    const data = await listAlerts(params)
    if (seq !== loadSeq.value) return
    rows.value = data.items ?? []
    total.value = data.total ?? 0
  } catch (err) {
    if (seq !== loadSeq.value) return
    rows.value = []
    total.value = 0
    showApiError(err, t('alert.loadFailed'))
  } finally {
    if (seq === loadSeq.value) loading.value = false
  }
}

const levels = computed<Array<{ value: AlertLevel; label: string; tag: 'primary' | 'success' | 'warning' | 'danger' | 'info' }>>(() => [
  { value: 'critical', label: t('alert.levelCritical'), tag: 'danger' },
  { value: 'error', label: t('alert.levelError'), tag: 'danger' },
  { value: 'warning', label: t('alert.levelWarning'), tag: 'warning' },
  { value: 'info', label: t('alert.levelInfo'), tag: 'info' }
])

function levelLabel(level: string) {
  return levels.value.find((l) => l.value === level)?.label || level
}

function levelTagType(level: string) {
  return levels.value.find((l) => l.value === level)?.tag || 'info'
}

function sourceLabel(source: string) {
  const key = `alert.source_${source}`
  const localized = t(key)
  // i18n 缺 key 时原样返回 key 本身，回退显示原始 source
  return localized === key ? source : localized
}

async function onResolve(row: AlertRecord) {
  try {
    await ElMessageBox.confirm(t('alert.confirmResolve', { title: row.title }), t('alert.resolveTitle'), {
      type: 'warning',
      confirmButtonText: t('alert.resolve'),
      cancelButtonText: t('common.cancel')
    })
  } catch {
    return // 用户取消
  }
  resolvingId.value = row.id
  try {
    await resolveAlert({ id: row.id })
    ElMessage.success(t('alert.resolvedOk'))
    await load()
  } catch (err) {
    showApiError(err, t('alert.resolveFailed'))
  } finally {
    resolvingId.value = ''
  }
}
</script>

<style scoped>
.alert-tabs {
  margin-top: 4px;
}

.alert-card {
  display: grid;
  gap: 6px;
}

.card-title {
  align-items: center;
  display: flex;
  gap: 8px;
  min-width: 0;
}

.card-source {
  color: var(--text-3);
  font-size: 12px;
}

.card-fields {
  color: var(--text-3);
  display: flex;
  flex-wrap: wrap;
  font-size: 12px;
  gap: var(--space-3);
}

.title-cell {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}
</style>
