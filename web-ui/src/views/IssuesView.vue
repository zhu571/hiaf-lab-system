<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('issues.title') }}</h2>
      <el-button type="primary" @click="createDialog = true">{{ t('issues.create') }}</el-button>
    </div>
    <!-- empty 不接线：看板 4 列 + 列内 empty-hint 即空态呈现，保持桌面布局零回归 -->
    <StateBlock :loading="loading && !data" :error="error" :error-text="t('issues.loadFailed')" @retry="run">
      <div class="board">
        <section v-for="status in statuses" :key="status" class="panel column">
          <div class="column-head">
            <h3><span class="dot" :style="{ background: statusDotColor(status) }" />{{ statusLabel(status) }}</h3>
            <span class="count">{{ grouped[status].length }}</span>
          </div>
          <button
            v-for="issue in grouped[status]"
            :key="issue.id"
            class="issue-card"
            @click="open(issue.id)"
          >
            <strong>{{ issue.title }}</strong>
            <StatusBadge class="severity-badge" domain="issueSeverity" :value="issue.severity" />
            <el-tag v-if="issue.ai_generated" class="ai-tag" size="small" type="warning" effect="light">AI</el-tag>
          </button>
          <p v-if="grouped[status].length === 0" class="empty-hint">{{ t('issues.empty') }}</p>
        </section>
      </div>
    </StateBlock>
    <el-pagination
      v-model:current-page="page"
      v-model:page-size="perPage"
      class="pager"
      layout="total, sizes, prev, pager, next"
      :page-sizes="[20, 50, 100]"
      :total="total"
      @current-change="run"
      @size-change="(n: number) => { onSizeChange(n); run() }"
    />
    <el-drawer v-model="drawer" size="420" :title="t('issues.detail')">
      <div v-if="selected" class="grid">
        <StatusBadge domain="issueStatus" :value="selected.status" />
        <h3>{{ selected.title }}</h3>
        <MarkdownView :source="selected.description" />
        <el-select v-model="targetStatus">
          <el-option v-for="s in statuses" :key="s" :label="statusLabel(s)" :value="s" />
        </el-select>
        <el-input v-model="reason" :placeholder="t('issues.reasonPlaceholder')" />
        <el-button @click="transition">{{ t('issues.updateStatus') }}</el-button>
        <CommentSection :comments="selected.comments || []" @submit="comment" />
      </div>
    </el-drawer>
    <el-dialog v-model="createDialog" :title="t('issues.create')" width="560">
      <el-form label-position="top">
        <el-form-item :label="t('issues.fieldTitle')"><el-input v-model="draft.title" /></el-form-item>
        <el-form-item :label="t('issues.severity')"><el-select v-model="draft.severity"><el-option v-for="s in severities" :key="s" :label="severityLabel(s)" :value="s" /></el-select></el-form-item>
        <el-form-item :label="t('issues.description')"><el-input v-model="draft.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="create">{{ t('issues.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '@/components/base/StatusBadge.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import CommentSection from '@/components/business/CommentSection.vue'
import MarkdownView from '@/components/business/MarkdownView.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import { statusMetaFor } from '@/utils/statusMeta'
import { addIssueComment, createIssue, getIssue, listIssues, transitionIssue, type Issue } from '../api/issues'
import { useProjectStore } from '../stores/project'

const route = useRoute()
const projects = useProjectStore()
const { t } = useI18n()
const selected = ref<Issue | null>(null)
const drawer = ref(false)
const createDialog = ref(false)
const targetStatus = ref('open')
const reason = ref('')
const statuses = ['open', 'in_progress', 'resolved', 'closed']
const severities = ['low', 'medium', 'high', 'critical']
const draft = reactive({ title: '', severity: 'medium', description: '' })

const projectId = computed(() => String(route.params.id || projects.current?.id || ''))

const { page, perPage, total, reset, onSizeChange } = usePagination({ perPage: 20 })

// 列表数据走 useAsyncData（内建竞态 seq + unmount 丢弃）；error 只写 ref，由 StateBlock 呈现
const { data, loading, error, run } = useAsyncData<Issue[]>(async () => {
  await projects.load()
  if (!projectId.value) return []
  const res = await listIssues(projectId.value, { page: page.value, per_page: perPage.value })
  total.value = res.total ?? 0
  return res.items ?? []
})

const grouped = computed(() => Object.fromEntries(statuses.map((s) => [s, (data.value ?? []).filter((item) => item.status === s)])) as Record<string, Issue[]>)

// 切换项目回第一页重拉；首屏加载由 useAsyncData immediate 承担，无需 onMounted
watch(projectId, () => {
  reset()
  run()
})

// 状态/严重度文案走 statusMeta 注册表 labelKey 国际化，未命中回退原文
function statusLabel(s: string) {
  const m = statusMetaFor('issueStatus', s)
  return m ? t(m.labelKey) : s
}

function severityLabel(s: string) {
  const m = statusMetaFor('issueSeverity', s)
  return m ? t(m.labelKey) : s
}

// 列头圆点色对齐 statusMeta 注册表 graphic（issueStatus 域），未命中回退 --info
function statusDotColor(s: string) {
  return `var(${statusMetaFor('issueStatus', s)?.graphic ?? '--info'})`
}

async function open(id: string) {
  try {
    selected.value = await getIssue(id)
    targetStatus.value = selected.value.status
    drawer.value = true
  } catch (err) {
    showApiError(err, t('issues.statusUpdateFailed'))
  }
}

async function transition() {
  if (!selected.value) return
  const id = selected.value.id
  try {
    await transitionIssue(id, targetStatus.value, reason.value)
    reason.value = ''
    ElMessage.success(t('issues.statusUpdated'))
    await run()
    await open(id)
  } catch (err) {
    showApiError(err, t('issues.statusUpdateFailed'))
  }
}

async function comment(content: string) {
  if (!selected.value) return
  try {
    await addIssueComment(selected.value.id, content)
    await open(selected.value.id)
  } catch (err) {
    showApiError(err, t('issues.commentFailed'))
  }
}

async function create() {
  try {
    await createIssue(projectId.value, draft)
    createDialog.value = false
    await run()
  } catch (err) {
    showApiError(err, t('issues.createFailed'))
  }
}
</script>

<style scoped>
.board {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.column {
  align-content: start;
  background: var(--surface-2);
  display: grid;
  gap: var(--space-3);
}

.column-head {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.column-head h3 {
  align-items: center;
  display: flex;
  font-size: 14px;
  gap: 8px;
  letter-spacing: 0.01em;
}

.dot {
  border-radius: 50%;
  height: 8px;
  width: 8px;
}

.count {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  color: var(--text-3);
  font-size: 12px;
  font-weight: 600;
  min-width: 26px;
  padding: 0 8px;
  text-align: center;
}

.issue-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  text-align: left;
  transition: var(--dur-base) var(--ease-standard);
}

.issue-card:hover {
  border-color: var(--brand-400);
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.issue-card strong {
  color: var(--text-1);
  font-size: 14px;
  line-height: 1.4;
}

/* StatusBadge（el-tag）作为 grid item 防拉伸，对齐 .ai-tag 的 justify-self 处理 */
.severity-badge {
  justify-self: start;
}

.ai-tag {
  justify-self: start;
}

.empty-hint {
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-md);
  color: var(--text-3);
  font-size: 12px;
  padding: 16px 0;
  text-align: center;
}

@media (max-width: 768px) {
  .board {
    grid-template-columns: 1fr;
  }
}
</style>
