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
          <ResponsiveTable :rows="events" :loading="eventsLoading">
            <el-table-column prop="created_at" :label="t('audit.time')" width="190" show-overflow-tooltip />
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
            <template #empty>
              <el-empty :description="t('audit.noRecords')" />
            </template>
            <template #card="{ row }">
              <div class="audit-card">
                <span class="card-title"><code class="audit-method">{{ row.method }}</code> {{ row.path }}</span>
                <div class="card-fields">
                  <span>{{ row.created_at }}</span>
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
            @size-change="onFilter"
          />
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
            <el-table-column prop="created_at" :label="t('audit.time')" width="190" />
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
                  <span>{{ row.created_at }}</span>
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
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { showApiError } from '../composables/useNotify'
import { useMobile } from '../composables/useMobile'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { getAudit, listAuditEvents, type AuditRecord } from '../api/audit'

const { t } = useI18n()
const isMobile = useMobile()

const tab = ref('events')

// 事件列表（C12 新增，消费 C7 的 /api/v1/audit/events）
const events = ref<AuditRecord[]>([])
const eventsLoading = ref(false)
const filterAction = ref('')
const filterUserId = ref('')
const filterActorType = ref('')
const timeRange = ref<[string, string] | null>(null)
const page = ref(1)
const perPage = ref(20)
const total = ref(0)

const actorTypes = computed(() => [
  { value: 'user', label: t('audit.actorUser') },
  { value: 'agent', label: t('audit.actorAgent') }
])

onMounted(loadEvents)

function actorLabel(v?: string) {
  return actorTypes.value.find((a) => a.value === v)?.label || v || '-'
}

function onFilter() {
  page.value = 1
  loadEvents()
}

async function loadEvents() {
  eventsLoading.value = true
  try {
    const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
    if (filterAction.value.trim()) params.action = filterAction.value.trim()
    if (filterUserId.value.trim()) params.user_id = filterUserId.value.trim()
    if (filterActorType.value) params.actor_type = filterActorType.value
    if (timeRange.value?.[0]) params.from = timeRange.value[0]
    if (timeRange.value?.[1]) params.to = timeRange.value[1]
    const data = await listAuditEvents(params)
    events.value = data.items ?? []
    total.value = data.total ?? 0
  } catch (err) {
    events.value = []
    total.value = 0
    showApiError(err, t('audit.loadFailed'))
  } finally {
    eventsLoading.value = false
  }
}

// 从列表跳转到 request_id 详情查询
function openByRequestId(id: string) {
  requestId.value = id
  tab.value = 'byRequestId'
  load()
}

// 按 request_id 查询（既有能力）
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
