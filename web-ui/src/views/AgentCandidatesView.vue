<template>
  <div class="page">
    <div class="toolbar">
      <h2>AI 候选审核</h2>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-select v-model="status" placeholder="状态" @change="onFilter">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
      </div>
    </section>
    <section class="panel">
      <el-table v-loading="loading" :data="candidates">
        <el-table-column label="日期" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            <el-tag :type="actionTag(row.action_type)" size="small" effect="light">{{ actionLabel(row.action_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="标题">
          <template #default="{ row }">{{ summary(row) }}</template>
        </el-table-column>
        <el-table-column label="置信度" width="100">
          <template #default="{ row }">{{ formatConfidence(row.agent_confidence) }}</template>
        </el-table-column>
        <el-table-column label="来源" width="110">
          <template #default>
            <RouterLink to="/daily-reports">来源日报</RouterLink>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220">
          <template #default="{ row }">
            <el-button size="small" @click="openDetail(row)">详情</el-button>
            <template v-if="row.status === 'pending_review'">
              <el-button size="small" type="primary" @click="approve(row)">批准</el-button>
              <el-button size="small" type="danger" @click="openReject(row)">拒绝</el-button>
            </template>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无候选动作" />
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

    <el-drawer v-model="drawer" size="460" title="候选详情">
      <div v-if="selected" class="detail">
        <el-tag :type="actionTag(selected.action_type)" size="small" effect="light">{{ actionLabel(selected.action_type) }}</el-tag>
        <h3>{{ selected.payload.title || '（无标题）' }}</h3>
        <p v-if="selected.payload.severity" class="meta">严重程度：{{ selected.payload.severity }}</p>
        <p v-if="selected.payload.description" class="desc">{{ selected.payload.description }}</p>
        <template v-if="selected.action_type === 'add_comment'">
          <p class="meta">目标 Issue：{{ selected.payload.issue_id || '-' }}</p>
          <p class="desc">{{ selected.payload.content }}</p>
        </template>
        <p v-if="selected.payload.is_duplicate" class="meta">
          疑似重复：{{ selected.payload.duplicate_issue_id || '（未指定 Issue）' }}
        </p>
        <el-descriptions border :column="1" size="small">
          <el-descriptions-item label="LLM 置信度">{{ formatConfidence(selected.agent_confidence) }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.prompt_version" label="Prompt 版本">{{ selected.prompt_version }}</el-descriptions-item>
          <el-descriptions-item label="状态"><StatusBadge :value="selected.status" /></el-descriptions-item>
          <el-descriptions-item v-if="selected.reviewed_by" label="审核人">{{ selected.reviewed_by }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.reviewed_at" label="审核时间">{{ formatTime(selected.reviewed_at) }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.review_reason" label="拒绝理由">{{ selected.review_reason }}</el-descriptions-item>
          <el-descriptions-item v-if="selected.execution_error" label="执行错误">{{ selected.execution_error }}</el-descriptions-item>
        </el-descriptions>
        <details class="raw">
          <summary>原始 payload</summary>
          <pre>{{ JSON.stringify(selected.payload, null, 2) }}</pre>
        </details>
        <div v-if="selected.status === 'pending_review'" class="actions">
          <el-button type="primary" @click="approve(selected)">批准</el-button>
          <el-button type="danger" @click="openReject(selected)">拒绝</el-button>
        </div>
      </div>
    </el-drawer>

    <el-dialog v-model="rejectDialog" title="拒绝候选" width="480">
      <el-input v-model="rejectReason" type="textarea" :rows="3" placeholder="拒绝理由（必填）" />
      <template #footer>
        <el-button @click="rejectDialog = false">取消</el-button>
        <el-button type="danger" :disabled="!rejectReason.trim()" @click="reject">确认拒绝</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { approveCandidate, listAgentCandidates, rejectCandidate, type AgentCandidate } from '../api/agent'

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

const statuses = [
  { value: 'pending_review', label: '待审核' },
  { value: 'approved', label: '已批准' },
  { value: 'rejected', label: '已拒绝' },
  { value: 'executed', label: '已执行' },
  { value: 'execution_failed', label: '执行失败' }
]

const actionTypes: Record<string, { label: string; tag: 'primary' | 'success' | 'warning' }> = {
  create_issue: { label: '创建Issue', tag: 'primary' },
  add_comment: { label: '追加评论', tag: 'success' },
  create_experience: { label: '创建经验', tag: 'warning' }
}

onMounted(load)

async function load() {
  loading.value = true
  try {
    const data = await listAgentCandidates({ status: status.value, page: page.value, per_page: perPage })
    candidates.value = data.items
    total.value = data.total
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '候选列表加载失败')
  } finally {
    loading.value = false
  }
}

function onFilter() {
  page.value = 1
  load()
}

function actionLabel(v: string) {
  return actionTypes[v]?.label || v
}

function actionTag(v: string) {
  return actionTypes[v]?.tag || 'primary'
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
    await ElMessageBox.confirm(`确定批准该候选（${actionLabel(row.action_type)}）吗？将创建对应业务记录。`, '批准确认', {
      confirmButtonText: '批准',
      cancelButtonText: '取消',
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await approveCandidate(row.id)
    ElMessage.success('已批准')
    drawer.value = false
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '批准失败')
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
    ElMessage.success('已拒绝')
    rejectDialog.value = false
    drawer.value = false
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '拒绝失败')
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
