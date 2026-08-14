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
      <ResponsiveTable :rows="candidates" :loading="loading">
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
        <template #card="{ row }">
          <div class="candidate-card">
            <span class="card-title">{{ summary(row) }}</span>
            <div class="card-fields">
              <span>
                <el-tag :type="actionTag(row.action_type)" size="small" effect="light">{{ actionLabel(row.action_type) }}</el-tag>
              </span>
              <span>{{ t('agentCandidates.confidence') }}：{{ formatConfidence(row.agent_confidence) }}</span>
              <span>{{ formatTime(row.created_at) }}</span>
            </div>
            <div class="card-actions">
              <el-button size="small" @click="openDetail(row)">{{ t('agentCandidates.viewDetail') }}</el-button>
              <template v-if="row.status === 'pending_review'">
                <el-button size="small" type="primary" @click="approve(row)">{{ t('agentCandidates.approve') }}</el-button>
                <el-button size="small" type="danger" @click="openReject(row)">{{ t('agentCandidates.reject') }}</el-button>
              </template>
            </div>
          </div>
        </template>
      </ResponsiveTable>
      <el-pagination
        v-model:current-page="page"
        class="pager"
        layout="total, prev, pager, next"
        :page-size="perPage"
        :total="total"
        @current-change="load"
      />
    </section>

    <el-drawer v-model="drawer" size="720" :title="t('agentCandidates.candidateDetail')">
      <div v-if="selected" class="detail">
        <div>
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
        </div>

        <!-- C16 动作流时间线：日报提交 → AI 解析 → 候选生成 → 人工审核 → 执行产物（数据源 C8 trace 端点） -->
        <el-timeline class="flow">
          <el-timeline-item type="primary" :timestamp="reportDateText">
            <h4>{{ t('agentCandidates.stageSubmit') }}</h4>
            <p v-if="selected.report_id" class="meta">
              <RouterLink :to="`/daily-reports/${selected.report_id}`">{{ t('agentCandidates.sourceReport') }}</RouterLink>
            </p>
          </el-timeline-item>
          <el-timeline-item :type="parseStageType" :timestamp="parseStageTime">
            <h4>{{ t('agentCandidates.stageAiParse') }}</h4>
            <p v-if="traceLoading" class="meta">{{ t('agentCandidates.diffLoading') }}</p>
            <template v-else-if="trace">
              <p class="meta">{{ t('agentCandidates.model') }}{{ trace.task.model || '-' }}</p>
              <p class="meta">{{ t('agentCandidates.promptVersion') }}：{{ trace.task.prompt_version || '-' }}</p>
              <p class="meta">{{ t('agentCandidates.llmConfidence') }}：{{ formatConfidence(trace.task.agent_confidence) }}</p>
              <p v-if="trace.task.raw_text_sha256" class="meta mono">SHA-256: {{ trace.task.raw_text_sha256 }}</p>
              <details v-if="trace.task.raw_text_snapshot" class="raw">
                <summary>{{ t('agentCandidates.snapshotExpand') }}</summary>
                <pre>{{ trace.task.raw_text_snapshot }}</pre>
              </details>
              <p v-else class="meta">{{ t('agentCandidates.noSnapshot') }}</p>
            </template>
            <p v-else class="meta">{{ t('agentCandidates.traceUnavailable') }}</p>
          </el-timeline-item>
          <el-timeline-item type="primary" :timestamp="formatTime(selected.created_at)">
            <h4>{{ t('agentCandidates.stageCandidate') }}</h4>
            <section v-if="diffLoading || diffSource != null || traceLoaded" class="diff-block">
              <div class="diff-head">
                <h5>{{ t('agentCandidates.diffTitle') }}</h5>
                <el-radio-group v-model="diffMode" size="small">
                  <el-radio-button value="line">{{ t('agentCandidates.diffModeLine') }}</el-radio-button>
                  <el-radio-button value="word">{{ t('agentCandidates.diffModeWord') }}</el-radio-button>
                </el-radio-group>
              </div>
              <p v-if="diffLoading" class="meta">{{ t('agentCandidates.diffLoading') }}</p>
              <template v-else-if="diffSource != null">
                <p class="meta">{{ diffSourceNote }}</p>
                <div class="diff-panes">
                  <div class="diff-pane">
                    <h5>{{ t('agentCandidates.diffSource') }}</h5>
                    <div class="diff-text"><template v-for="(p, i) in diffParts" :key="i"><del v-if="p.removed" class="diff-del">{{ p.value }}</del><span v-else-if="!p.added">{{ p.value }}</span></template></div>
                  </div>
                  <div class="diff-pane">
                    <h5>{{ t('agentCandidates.diffCandidate') }}</h5>
                    <div class="diff-text"><template v-for="(p, i) in diffParts" :key="i"><ins v-if="p.added" class="diff-ins">{{ p.value }}</ins><span v-else-if="!p.removed">{{ p.value }}</span></template></div>
                  </div>
                </div>
              </template>
              <p v-else class="meta">{{ t('agentCandidates.diffNoSource') }}</p>
            </section>
          </el-timeline-item>
          <el-timeline-item :type="reviewStageType" :timestamp="formatStageTime(selected.reviewed_at)">
            <h4>
              {{ t('agentCandidates.stageReview') }}
              <el-tag v-if="selected.status === 'pending_review'" type="warning" size="small">{{ t('agentCandidates.reviewPending') }}</el-tag>
              <StatusBadge v-else :value="selected.status" />
            </h4>
            <template v-if="selected.status !== 'pending_review'">
              <p v-if="selected.reviewed_by" class="meta">{{ t('agentCandidates.reviewer') }}：{{ selected.reviewed_by }}</p>
              <p v-if="selected.review_reason" class="meta">{{ t('agentCandidates.rejectReason') }}：{{ selected.review_reason }}</p>
            </template>
          </el-timeline-item>
          <el-timeline-item :type="resultStageType" :timestamp="formatStageTime(selected.executed_at)">
            <h4>{{ t('agentCandidates.stageResult') }}</h4>
            <p v-if="selected.status === 'executed' && trace?.result" class="meta">
              <RouterLink :to="trace.result.url">{{ trace.result.title || t('agentCandidates.viewResult') }}</RouterLink>
            </p>
            <p v-else-if="selected.status === 'executed'" class="meta">{{ t('agentCandidates.statusExecuted') }}</p>
            <p v-else-if="selected.status === 'execution_failed'" class="meta error">
              {{ t('agentCandidates.executionError') }}：{{ selected.execution_error || '-' }}
            </p>
            <p v-else-if="selected.status === 'rejected'" class="meta">{{ t('agentCandidates.resultRejected') }}</p>
            <p v-else class="meta">{{ t('agentCandidates.resultPending') }}</p>
          </el-timeline-item>
        </el-timeline>

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
import { diffLines, diffWordsWithSpace, type Change } from 'diff'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '@/components/base/StatusBadge.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { approveCandidate, getCandidateTrace, listAgentCandidates, rejectCandidate, type AgentCandidate, type CandidateTrace } from '../api/agent'

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
// C13 diff 对照：左栏为来源日报原文（优先 C8 trace 快照，降级日报当前值），右栏为候选 payload 文本
const traceLoaded = ref(false)
const diffLoading = ref(false)
const diffSource = ref<string | null>(null)
const diffSourceNote = ref('')
const diffMode = ref<'line' | 'word'>('line')
// C16 动作流时间线：完整 trace（任务快照/审计/产物）；加载失败时各阶段降级显示
const trace = ref<CandidateTrace | null>(null)
const traceLoading = ref(false)

// raw_text 段落定位用简单截取（不做语义对齐），限定长度保证 diff 性能
const DIFF_SOURCE_LIMIT = 2000

const diffParts = computed<Change[]>(() => {
  if (!selected.value || diffSource.value == null) return []
  const right = payloadText(selected.value)
  return diffMode.value === 'word' ? diffWordsWithSpace(diffSource.value, right) : diffLines(diffSource.value, right)
})

function payloadText(c: AgentCandidate): string {
  const p = c.payload
  return [p.title, p.description, p.content].filter((v): v is string => Boolean(v && String(v).trim())).join('\n\n')
}

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

// 时间线阶段时间戳：缺省给空串（el-timeline-item 不渲染），区别于列表的 '-' 兜底
function formatStageTime(v?: string) {
  return v ? formatTime(v) : ''
}

type TimelineType = 'primary' | 'success' | 'warning' | 'danger' | 'info'

// ① 日报提交：优先任务快照日期（AI 当时所见），降级日报当前值；均无则空（存量任务）
const reportDateText = computed(() => trace.value?.task.report_date || trace.value?.report?.report_date || '')

// ② AI 解析：完成时间取审计链 complete/fail 行；状态映射时间线颜色
const parseStageTime = computed(() => {
  const evt = trace.value?.audit?.find((a) => a.action.endsWith('.complete') || a.action.endsWith('.fail'))
  return evt ? formatStageTime(evt.created_at) : ''
})
const parseStageType = computed<TimelineType>(() => {
  const s = trace.value?.task.status
  if (s === 'done') return 'success'
  if (s === 'failed' || s === 'dead') return 'danger'
  return 'info'
})

// ④ 人工审核：待审核高亮待办（warning）
const reviewStageType = computed<TimelineType>(() => {
  if (!selected.value || selected.value.status === 'pending_review') return 'warning'
  if (selected.value.status === 'rejected') return 'info'
  return 'success'
})

// ⑤ 执行产物
const resultStageType = computed<TimelineType>(() => {
  if (!selected.value) return 'info'
  if (selected.value.status === 'executed') return 'success'
  if (selected.value.status === 'execution_failed') return 'danger'
  return 'info'
})

function openDetail(row: AgentCandidate) {
  selected.value = row
  drawer.value = true
  loadTrace(row.id)
}

async function loadTrace(id: string) {
  traceLoaded.value = false
  traceLoading.value = true
  trace.value = null
  diffLoading.value = true
  diffSource.value = null
  diffSourceNote.value = ''
  diffMode.value = 'line'
  try {
    const data = await getCandidateTrace(id)
    trace.value = data
    const snapshot = data.task.raw_text_snapshot?.trim() ? data.task.raw_text_snapshot : null
    const raw = snapshot ?? (data.report?.raw_text?.trim() ? data.report.raw_text : null)
    if (raw != null) {
      // 快照是"AI 当时看到的内容"（更准）；存量任务无快照时降级日报当前值并明示
      diffSourceNote.value = t(snapshot ? 'agentCandidates.diffFromSnapshot' : 'agentCandidates.diffFromCurrent')
      if (raw.length > DIFF_SOURCE_LIMIT) {
        diffSourceNote.value += t('agentCandidates.diffTruncated')
        diffSource.value = raw.slice(0, DIFF_SOURCE_LIMIT)
      } else {
        diffSource.value = raw
      }
    }
    traceLoaded.value = true
  } catch (err) {
    showApiError(err, t('agentCandidates.diffLoadFailed'))
  } finally {
    traceLoading.value = false
    diffLoading.value = false
  }
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

.flow {
  padding-left: 4px;
}

.flow h4 {
  align-items: center;
  color: var(--text-1);
  display: flex;
  font-size: 14px;
  gap: 8px;
}

.mono {
  font-family: ui-monospace, monospace;
  word-break: break-all;
}

.error {
  color: var(--danger);
}

.meta {
  color: var(--text-3);
  font-size: 13px;
}

.desc {
  color: var(--text-2);
  white-space: pre-wrap;
}

.diff-block {
  display: grid;
  gap: 10px;
}

.diff-head {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.diff-head h4 {
  color: var(--text-1);
  font-size: 14px;
}

.diff-panes {
  display: grid;
  gap: 10px;
  grid-template-columns: 1fr 1fr;
}

.diff-pane h5 {
  color: var(--text-3);
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 6px;
}

.diff-text {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 320px;
  overflow: auto;
  padding: 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

.diff-ins {
  background: rgba(46, 160, 67, 0.2);
  border-radius: 2px;
  text-decoration: none;
}

.diff-del {
  background: rgba(218, 54, 51, 0.18);
  border-radius: 2px;
  text-decoration: line-through;
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

  .diff-panes {
    grid-template-columns: 1fr;
  }
}
</style>
