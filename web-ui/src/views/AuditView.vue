<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('audit.title') }}</h2>
    </div>
    <el-tabs v-model="tab" class="audit-tabs">
      <el-tab-pane :label="t('audit.tabEvents')" name="events">
        <section class="panel filters-panel">
          <div class="filters">
            <el-input v-model="filterAction" :placeholder="t('audit.action')" clearable class="f-action" @change="onFilter" />
            <el-input v-model="filterUserId" :placeholder="t('audit.filterUserId')" clearable class="f-user" @change="onFilter" />
            <el-select v-model="filterActorType" :placeholder="t('audit.actorType')" clearable class="f-actor" @change="onFilter">
              <el-option v-for="a in actorTypes" :key="a.value" :label="a.label" :value="a.value" />
            </el-select>
            <el-date-picker
              v-model="timeRange"
              type="datetimerange"
              value-format="YYYY-MM-DDTHH:mm:ssZ"
              :start-placeholder="t('audit.rangeStart')"
              :end-placeholder="t('audit.rangeEnd')"
              class="f-range"
              @change="onFilter"
            />
          </div>
        </section>
        <section class="panel">
          <!-- 列表三态收敛 StateBlock（S4）：首屏骨架 > 错误 > 空态 > 表格；翻页/筛选刷新时走表格 v-loading，不闪骨架 -->
          <StateBlock
            :loading="eventsLoading && !eventsData"
            :error="eventsError"
            :empty="events.length === 0"
            :error-text="t('audit.loadFailed')"
            :empty-text="t('audit.noRecords')"
            @retry="loadEvents"
          >
            <ResponsiveTable :rows="events" :loading="eventsLoading">
              <el-table-column :label="t('audit.time')" width="190" show-overflow-tooltip>
                <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
              </el-table-column>
              <el-table-column :label="t('audit.actorType')" width="100">
                <template #default="{ row }">{{ actorLabel(row.actor_type) }}</template>
              </el-table-column>
              <el-table-column prop="username" :label="t('audit.user')" width="120" show-overflow-tooltip />
              <el-table-column prop="method" :label="t('audit.method')" width="90" />
              <el-table-column prop="path" :label="t('audit.path')" show-overflow-tooltip />
              <el-table-column prop="status_code" :label="t('audit.status')" width="90" />
              <el-table-column prop="action" :label="t('audit.action')" show-overflow-tooltip />
              <el-table-column :label="t('audit.requestIdCol')" width="130">
                <template #default="{ row }">
                  <el-button link type="primary" size="small" @click="openByRequestId(row.request_id)">{{ row.request_id.slice(0, 12) }}…</el-button>
                </template>
              </el-table-column>
              <template #card="{ row }">
                <div class="audit-card">
                  <span class="card-title"><code class="audit-method">{{ row.method }}</code> {{ row.path }}</span>
                  <div class="card-fields">
                    <span>{{ formatDateTime(row.created_at) }}</span>
                    <span>HTTP {{ row.status_code }}</span>
                    <span>{{ row.username || '-' }}</span>
                    <span>{{ actorLabel(row.actor_type) }}</span>
                  </div>
                </div>
              </template>
            </ResponsiveTable>
            <el-pagination
              v-model:current-page="page"
              v-model:page-size="perPage"
              class="pager"
              layout="total, sizes, prev, pager, next"
              :page-sizes="[20, 50, 100]"
              :total="total"
              @current-change="loadEvents"
              @size-change="onPageSizeChange"
            />
          </StateBlock>
        </section>
      </el-tab-pane>
      <el-tab-pane :label="t('audit.tabByRequestId')" name="byRequestId">
        <section class="panel">
          <div class="query-group">
            <el-input v-model="requestId" placeholder="request_id" clearable class="request-input" @keyup.enter="load" />
            <el-button type="primary" @click="load">{{ t('audit.query') }}</el-button>
          </div>
          <el-descriptions v-if="records[0]" border :column="isMobile ? 1 : 2">
            <el-descriptions-item label="Request ID">{{ records[0].request_id }}</el-descriptions-item>
            <el-descriptions-item :label="t('audit.recordCount')">{{ records.length }}</el-descriptions-item>
            <el-descriptions-item :label="t('audit.user')">{{ records[0].username || '-' }}</el-descriptions-item>
            <el-descriptions-item :label="t('audit.client')">{{ records[0].client_ip || '-' }}</el-descriptions-item>
          </el-descriptions>
          <el-empty v-else :description="t('audit.inputRequestId')" />
        </section>
        <section class="panel">
          <ResponsiveTable :rows="records">
            <el-table-column :label="t('audit.time')" width="190">
              <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="method" :label="t('audit.method')" width="90" />
            <el-table-column prop="path" :label="t('audit.path')" />
            <el-table-column prop="status_code" :label="t('audit.status')" width="90" />
            <el-table-column prop="action" :label="t('audit.action')" />
            <template #empty>
              <el-empty :description="t('audit.noRecords')" />
            </template>
            <template #card="{ row }">
              <div class="audit-card">
                <span class="card-title"><code class="audit-method">{{ row.method }}</code> {{ row.path }}</span>
                <div class="card-fields">
                  <span>{{ formatDateTime(row.created_at) }}</span>
                  <span>HTTP {{ row.status_code }}</span>
                  <span>{{ row.username || '-' }}</span>
                </div>
              </div>
            </template>
          </ResponsiveTable>
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import { getAudit, listAuditEvents, type AuditRecord } from '@/api/audit'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import { useMobile } from '@/composables/useMobile'
import { usePagination } from '@/composables/usePagination'
import { formatDateTime } from '@/utils/datetime'

const { t } = useI18n()
const isMobile = useMobile()

const tab = ref('events')

// 事件列表（C12 新增，消费 C7 的 /api/v1/audit/events）
const filterAction = ref('')
const filterUserId = ref('')
const filterActorType = ref('')
const timeRange = ref<[string, string] | null>(null)

// 分页状态（S4 收敛 usePagination）：perPage 保持原值 20；筛选变化只重置页码（对齐原 onFilter 行为）
const { page, perPage, total, onSizeChange } = usePagination({ perPage: 20 })

// 列表数据（S4 收敛 useAsyncData）：immediate 自动首载；列表级错误只写 error ref，交 StateBlock 呈现，不再 toast
const {
  data: eventsData,
  loading: eventsLoading,
  error: eventsError,
  run: loadEvents
} = useAsyncData(async () => {
  const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
  if (filterAction.value.trim()) params.action = filterAction.value.trim()
  if (filterUserId.value.trim()) params.user_id = filterUserId.value.trim()
  if (filterActorType.value) params.actor_type = filterActorType.value
  if (timeRange.value?.[0]) params.from = timeRange.value[0]
  if (timeRange.value?.[1]) params.to = timeRange.value[1]
  const res = await listAuditEvents(params)
  total.value = res.total ?? 0
  return res.items ?? []
})

const events = computed(() => eventsData.value ?? [])

const actorTypes = computed(() => [
  { value: 'user', label: t('audit.actorUser') },
  { value: 'agent', label: t('audit.actorAgent') }
])

function actorLabel(v?: string) {
  return actorTypes.value.find((a) => a.value === v)?.label || v || '-'
}

function onFilter() {
  page.value = 1
  loadEvents()
}

// el-pagination @size-change：每页条数切换（onSizeChange 内已重置 page=1；提到 script 以显式标注参数类型）
function onPageSizeChange(n: number) {
  onSizeChange(n)
  loadEvents()
}

// 从列表跳转到 request_id 详情查询
function openByRequestId(id: string) {
  requestId.value = id
  tab.value = 'byRequestId'
  load()
}

// 按 request_id 查询（既有能力；按钮触发的操作级查询，错误仍走 showApiError toast，不进 StateBlock）
const requestId = ref('')
const records = ref<AuditRecord[]>([])

async function load() {
  if (!requestId.value) return
  try {
    const data = await getAudit(requestId.value)
    records.value = data.items ?? []
  } catch (err) {
    records.value = []
    showApiError(err, t('audit.loadFailed'))
  }
}
</script>

<style scoped>
.audit-tabs {
  margin-top: 4px;
}

.filters-panel {
  padding: 14px 20px;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.filters .f-action {
  width: 200px;
}

.filters .f-user {
  width: 220px;
}

.filters .f-actor {
  width: 140px;
}

.filters .f-range {
  max-width: 360px;
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.query-group {
  align-items: center;
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}

.request-input {
  max-width: 420px;
}

.audit-method {
  background: var(--bg);
  border-radius: 4px;
  font-size: 12px;
  padding: 1px 6px;
}

@media (max-width: 768px) {
  .filters .f-action,
  .filters .f-user,
  .filters .f-actor,
  .filters .f-range {
    max-width: none;
    width: 100%;
  }

  .request-input {
    max-width: none;
  }
}
</style>
