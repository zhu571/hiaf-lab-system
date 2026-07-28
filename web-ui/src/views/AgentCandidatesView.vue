<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('agentCandidates.pageTitle') }}</h2>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-select v-model="status" :placeholder="t('agentCandidates.statusFilter')" @change="onFilter">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
      </div>
    </section>
    <section class="panel">
      <el-table v-loading="loading" :data="candidates">
        <el-table-column :label="t('agentCandidates.date')" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('agentCandidates.type')" width="120">
          <template #default="{ row }">
            <el-tag :type="actionTag(row.action_type)" size="small" effect="light">{{ actionLabel(row.action_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('agentCandidates.title')">
          <template #default="{ row }">{{ summary(row) }}</template>
        </el-table-column>
        <el-table-column :label="t('agentCandidates.confidence')" width="100">
          <template #default="{ row }">{{ formatConfidence(row.agent_confidence) }}</template>
        </el-table-column>
        <el-table-column :label="t('agentCandidates.source')" width="110">
          <template #default="{ row }">
            <RouterLink v-if="row.report_id" :to="`/daily-reports/${row.report_id}`">{{ t('agentCandidates.sourceReport') }}</RouterLink>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('agentCandidates.actions')" width="220">
          <template #default="{ row }">
            <el-button size="small" @click="openDetail(row)">{{ t('agentCandidates.viewDetail') }}</el-button>
            <template v-if="row.status === 'pending_review'">
              <el-button size="small" type="primary" @click="approve(row)">{{ t('agentCandidates.approve') }}</el-button>
              <el-button size="small" type="danger" @click="openReject(row)">{{ t('agentCandidates.reject') }}</el-button>
            </template>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('agentCandidates.empty')" />
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
    </section>

    <el-drawer v-model="drawer" size="460" :title="t('agentCandidates.candidateDetail')">
      <div v-if="selected" class="detail">
        <el-tag :type="actionTag(selected.action_type)" size="small" effect="light">{{ actionLabel(selected.action_type) }}</el-tag>
        <h3>{{ selected.payload.title || t('agentCandidates.noTitle') }}</h3>
        <p v-if="selected.payload.severity" class="meta">{{ t('agentCandidates.severity') }}{{ selected.payload.severity }}</p>
        <p v-if="selected.payload.description" class="desc">{{ selected.payload.description }}</p>
        <template v-if="selected.action_type === 'add_comment'">
          <p class="meta">{{ t('agentCandidates.targetIssue') }}{{ selected.payload.issue_id || '-' }}</p>
          <p class="desc">{{ selected.payload.content }}</p>
        </template>
        <p v-if="selected.payload.is_duplicate" class="meta">
          {{ t('agentCandidates.possibleDuplicate') }}{{ selected.payload.duplicate_issue_id || t('agentCandidates.noIssueSpecified') }}
        </p>
        <el-descriptions border :column="1" size="small">
          <el-descriptions-item :label="t('agentCandidates.llmConfidence')">{{ formatConfidence(selected.agent_confidence) }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.prompt_version" :label="t('agentCandidates.promptVersion')">{{ selected.prompt_version }}</el-descriptions-item>
          <el-descriptions-item :label="t('agentCandidates.status')"><StatusBadge :value="selected.status" /></el-descriptions-item>
          <el-descriptions-item v-if="selected.reviewed_by" :label="t('agentCandidates.reviewer')">{{ selected.reviewed_by }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.reviewed_at" :label="t('agentCandidates.reviewedAt')">{{ formatTime(selected.reviewed_at) }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.review_reason" :label="t('agentCandidates.rejectReason')">{{ selected.review_reason }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.execution_error" :label="t('agentCandidates.executionError')">{{ selected.execution_error }}</el-descriptions-item>
        </el-descriptions>
        <details class="raw">
          <summary>{{ t('agentCandidates.rawPayload') }}</summary>
          <pre>{{ JSON.stringify(selected.payload, null, 2) }}</pre>
        </details>
        <div v-if="selected.status === 'pending_review'" class="actions">
          <el-button type="primary" @click="approve(selected)">{{ t('agentCandidates.approve') }}</el-button>
          <el-button type="danger" @click="openReject(selected)">{{ t('agentCandidates.reject') }}</el-button>
        </div>
      </div>
    </el-drawer>

    <el-dialog v-model="rejectDialog" :title="t('agentCandidates.rejectCandidateTitle')" width="480">
      <el-input v-model="rejectReason" type="textarea" :rows="3" :placeholder="t('agentCandidates.rejectReasonRequired')" />
      <template #footer>
        <el-button @click="rejectDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="danger" :disabled="!rejectReason.trim()" @click="reject">{{ t('agentCandidates.confirmReject') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '../components/StatusBadge.vue'
import { approveCandidate, listAgentCandidates, rejectCandidate, type AgentCandidate } from '../api/agent'

const { t } = useI18n()
const status = ref('pending_review')
const candidates = ref<AgentCandidate[]>([])
const loading = ref(false)
const page = ref(1)
const perPage = 20
const total = ref(0)
const drawer = ref(false)
const selected = ref<AgentCandidate | null>(null)
const rejectDialog = ref(false)
const rejectReason = ref('')
const rejectTarget = ref<AgentCandidate | null>(null)

const statuses = computed(() => [
  { value: 'pending_review', label: t('agentCandidates.statusPending') },
  { value: 'approved', label: t('agentCandidates.statusApproved') },
  { value: 'rejected', label: t('agentCandidates.statusRejected') },
  { value: 'executed', label: t('agentCandidates.statusExecuted') },
  { value: 'execution_failed', label: t('agentCandidates.statusExecutionFailed') }
])

const actionTypes = computed<Record<string, { label: string; tag: 'primary' | 'success' | 'warning' }>>(() => ({
  create_issue: { label: t('agentCandidates.actionCreateIssue'), tag: 'primary' as const },
  add_comment: { label: t('agentCandidates.actionAddComment'), tag: 'success' as const },
  create_experience: { label: t('agentCandidates.actionCreateExperience'), tag: 'warning' as const }
}))

onMounted(load)

async function load() {
  loading.value = true
  try {
    const data = await listAgentCandidates({ status: status.value, page: page.value, per_page: perPage })
    candidates.value = data.items ?? []
    total.value = data.total
  } catch (err) {
    showApiError(err, t('agentCandidates.loadFailed'))
  } finally {
    loading.value = false
  }
}

function onFilter() {
  page.value = 1
  load()
}

function actionLabel(v: string) {
  return actionTypes.value[v]?.label || v
}

function actionTag(v: string) {
  return actionTypes.value[v]?.tag || 'primary'
}

function summary(row: AgentCandidate) {
  return row.payload.title || row.payload.content || row.payload.description || '-'
}

function formatConfidence(v?: number) {
  return v == null ? '-' : `${Math.round(v * 100)}%`
}

function formatTime(v?: string) {
  return v ? v.slice(0, 16).replace('T', ' ') : '-'
}

function openDetail(row: AgentCandidate) {
  selected.value = row
  drawer.value = true
}

async function approve(row: AgentCandidate) {
  try {
    await ElMessageBox.confirm(
      t('agentCandidates.confirmApproveMsg', { action: actionLabel(row.action_type) }),
      t('agentCandidates.confirmApproveTitle'),
      {
        confirmButtonText: t('agentCandidates.approve'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
  } catch {
    return
  }
  try {
    await approveCandidate(row.id)
    ElMessage.success(t('agentCandidates.approved'))
    drawer.value = false
    await load()
  } catch (err) {
    showApiError(err, t('agentCandidates.approveFailed'))
  }
}

function openReject(row: AgentCandidate) {
  rejectTarget.value = row
  rejectReason.value = ''
  rejectDialog.value = true
}

async function reject() {
  if (!rejectTarget.value) return
  try {
    await rejectCandidate(rejectTarget.value.id, rejectReason.value.trim())
    ElMessage.success(t('agentCandidates.rejected'))
    rejectDialog.value = false
    drawer.value = false
    await load()
  } catch (err) {
    showApiError(err, t('agentCandidates.rejectFailed'))
  }
}
</script>

<style scoped>
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

.detail {
  display: grid;
  gap: 12px;
}

.detail h3 {
  color: var(--text-1);
  font-size: 16px;
}

.meta {
  color: var(--text-3);
  font-size: 13px;
}

.desc {
  color: var(--text-2);
  white-space: pre-wrap;
}

.raw summary {
  color: var(--text-3);
  cursor: pointer;
  font-size: 12px;
}

.raw pre {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
  overflow: auto;
  padding: 10px;
}

.actions {
  display: flex;
  gap: 10px;
}

@media (max-width: 768px) {
  .filters .el-select {
    width: 100%;
  }
}
</style>
