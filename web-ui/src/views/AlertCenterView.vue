<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('alert.title') }}</h2>
    </div>
    <el-tabs v-model="tab" class="alert-tabs" @tab-change="onTabChange">
      <el-tab-pane :label="t('alert.tabActive')" name="active">
        <section class="panel">
          <!-- 三态收敛 StateBlock：首屏骨架（翻页/切 tab 有数据时不闪骨架）> 错误（可重试）> 空态 > 表格 -->
          <StateBlock
            :loading="loading && !data"
            :error="error"
            :error-text="t('alert.loadFailed')"
            :empty="rows.length === 0"
            :empty-text="t('alert.emptyActive')"
            @retry="run"
          >
            <ResponsiveTable :rows="rows" :loading="loading">
              <el-table-column :label="t('alert.level')" width="100">
                <template #default="{ row }">
                  <StatusBadge domain="alertLevel" :value="row.level" />
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
              <template #card="{ row }">
                <div class="alert-card">
                  <span class="card-title">
                    <StatusBadge domain="alertLevel" :value="row.level" />
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
          </StateBlock>
        </section>
      </el-tab-pane>
      <el-tab-pane :label="t('alert.tabHistory')" name="resolved">
        <section class="panel">
          <!-- 三态收敛 StateBlock：首屏骨架（翻页有数据时不闪骨架）> 错误（可重试）> 空态 > 表格 -->
          <StateBlock
            :loading="loading && !data"
            :error="error"
            :error-text="t('alert.loadFailed')"
            :empty="rows.length === 0"
            :empty-text="t('alert.emptyHistory')"
            @retry="run"
          >
            <ResponsiveTable :rows="rows" :loading="loading">
              <el-table-column :label="t('alert.level')" width="100">
                <template #default="{ row }">
                  <StatusBadge domain="alertLevel" :value="row.level" />
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
              <template #card="{ row }">
                <div class="alert-card">
                  <span class="card-title">
                    <StatusBadge domain="alertLevel" :value="row.level" />
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
          </StateBlock>
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="perPage"
            class="pager"
            layout="total, prev, pager, next"
            :total="total"
            @current-change="run"
            @size-change="onPageSizeChange"
          />
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { listAlerts, resolveAlert, type AlertRecord } from '@/api/alerts'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

// resolve 仅 admin/maintainer（后端 RequireRoleOrService 强校验，按钮隐藏只是 UX）
const canResolve = computed(() => auth.isAdmin || auth.user?.role === 'maintainer')

const tab = ref<'active' | 'resolved'>('active')
const resolvingId = ref('')

// 历史 tab 仅展示 90 天内（后端滚动清理），每页 50 条 + offset 简单分页
const { page, perPage, total, setTotal, reset, onSizeChange } = usePagination({ perPage: 50 })

// 列表加载收敛 useAsyncData：内建竞态 seq 替代原手写 loadSeq 递增守卫，
// 切 tab/翻页时过期响应自动丢弃（语义不变）；error 只写 ref，列表错误走 StateBlock，不再 toast
const { data, loading, error, run } = useAsyncData(async () => {
  const status = tab.value
  const params: Record<string, string | number> = { status }
  if (status === 'resolved') {
    params.limit = perPage.value
    params.offset = (page.value - 1) * perPage.value
  } else {
    params.limit = 100
  }
  try {
    const res = await listAlerts(params)
    setTotal(res.total ?? 0)
    return res
  } catch (err) {
    // 保持原 catch 语义：加载失败时清空 total
    setTotal(0)
    throw err
  }
})

const rows = computed(() => data.value?.items ?? [])

function onTabChange() {
  reset() // 切 tab 回第一页并清空 total
  run()
}

// 每页条数切换（onSizeChange 内已重置 page=1，随后重新加载）
function onPageSizeChange(n: number) {
  onSizeChange(n)
  run()
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
    await run()
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
