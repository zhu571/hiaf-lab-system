<template>
  <section class="panel dashboard">
    <div v-if="store.current" class="grid">
      <div class="toolbar">
        <div class="dash-title">
          <h2>{{ store.current.name }}</h2>
          <p class="muted">{{ store.current.description || t('projectDashboard.noDescription') }}</p>
        </div>
      </div>
      <div class="stage-panel">
        <div class="stage-flow">
          <template v-for="(stage, index) in STAGES" :key="stage.key">
            <div class="stage-node" :data-state="nodeState(index)">
              <span class="stage-badge" aria-hidden="true">{{ nodeState(index) === 'done' ? '✓' : stage.icon }}</span>
              <span class="stage-name">{{ stage.label }}</span>
              <el-button
                v-if="index === currentIndex && backAction"
                class="stage-back"
                :type="backAction.type"
                size="small"
                round
                plain
                @click="openBackConfirm"
              >
                <span class="back-arrow" aria-hidden="true">←</span>
                {{ backAction.label }}
              </el-button>
              <el-button
                v-if="index === currentIndex && nextAction"
                class="stage-next"
                type="primary"
                size="small"
                round
                @click="openConfirm"
              >
                {{ nextAction.label }}
                <span class="next-arrow" aria-hidden="true">→</span>
              </el-button>
            </div>
            <span v-if="index < STAGES.length - 1" class="stage-arrow" :data-done="index < currentIndex" aria-hidden="true">→</span>
          </template>
        </div>
        <div class="stage-desc">
          <strong class="stage-desc-title">{{ t('projectDashboard.currentStage', { label: currentStage.label }) }}</strong>
          <p v-for="item in stageDesc.can" :key="'can-' + item" class="desc-line">✅ {{ item }}</p>
          <p v-for="item in stageDesc.cannot" :key="'no-' + item" class="desc-line">❌ {{ item }}</p>
        </div>
      </div>
      <div class="metric-grid">
        <div class="metric"><strong>{{ members.length }}</strong><span>{{ t('projectDashboard.metricMembers') }}</span></div>
        <div class="metric"><strong>{{ issueTotal }}</strong><span>{{ t('projectDashboard.metricIssues') }}</span></div>
        <div class="metric"><strong>{{ store.current.log_count ?? logs.length }}</strong><span>{{ t('projectDashboard.metricLogs') }}</span></div>
      </div>
      <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" />
      <div v-loading="loading" class="overview-grid">
        <section class="overview-card">
          <div class="toolbar overview-head">
            <h3>{{ t('projectDashboard.projectMembers') }}（{{ members.length }}）</h3>
            <el-button v-if="canManageMembers" type="primary" size="small" @click="openAddMember">{{ t('projectDashboard.memberManagement.add') }}</el-button>
            <el-button v-if="auth.isAdmin" link type="primary" @click="go('/admin/users')">{{ t('projectDashboard.userManagement') }}</el-button>
          </div>
          <div v-if="canManageMembers && members.length" class="member-filters">
            <el-input v-model="memberKeyword" clearable :placeholder="t('projectDashboard.memberManagement.search')" />
            <el-select v-model="memberRoleFilter" :placeholder="t('projectDashboard.memberManagement.allRoles')" clearable>
              <el-option v-for="role in memberRoles" :key="role" :label="roleLabel(role)" :value="role" />
            </el-select>
          </div>
          <div v-if="members.length" class="member-list">
            <div v-for="member in filteredMembers" :key="member.user_id" class="member-row">
              <span>{{ memberName(member) }}</span>
              <el-tag size="small" effect="plain">{{ roleLabel(member.role) }}</el-tag>
              <span v-if="canManageMembers" class="member-actions">
                <el-button size="small" @click="openEditMember(member)">{{ t('projectDashboard.memberManagement.editRole') }}</el-button>
                <el-button size="small" type="danger" plain :disabled="isLastOwner(member)" @click="remove(member)">{{ t('projectDashboard.memberManagement.remove') }}</el-button>
              </span>
            </div>
          </div>
          <el-empty v-else :image-size="52" :description="t('projectDashboard.noMembers')" />
        </section>
        <section class="overview-card timeline-card">
          <div class="toolbar overview-head">
            <h3>{{ t('projectDashboard.recentLogs') }}</h3>
            <div class="overview-head-actions">
              <el-button link type="primary" @click="go('/projects/' + store.current.id + '/logs')">{{ t('projectDashboard.viewAllLogs') }}</el-button>
              <el-button link type="primary" @click="go('/daily-report')">{{ t('projectDashboard.newLog') }}</el-button>
            </div>
          </div>
          <div v-if="logs.length" class="timeline-list">
            <article v-for="log in logs" :key="log.id" class="timeline-item">
              <div class="timeline-meta">
                <el-tag size="small" effect="plain">{{ categoryLabel(log.category) }}</el-tag>
                <time>{{ formatDateTime(log.occurred_at) }}</time>
              </div>
              <p>{{ localized(log).text }}</p>
            </article>
          </div>
          <el-empty v-else :image-size="52" :description="t('projectDashboard.noLogs')" />
        </section>
        <section class="overview-card">
          <div class="toolbar overview-head">
            <h3>{{ t('projectDashboard.recentIssues') }}</h3>
            <el-button link type="primary" @click="go('/projects/' + store.current.id + '/issues')">{{ t('projectDashboard.newIssue') }}</el-button>
          </div>
          <div v-if="issues.length" class="issue-list">
            <div v-for="issue in issues" :key="issue.id" class="issue-row">
              <span>{{ issue.title }}</span>
              <el-tag size="small" effect="dark" class="severity-tag" :data-severity="issue.severity">{{ severityLabel(issue.severity) }}</el-tag>
            </div>
          </div>
          <el-empty v-else :image-size="52" :description="t('projectDashboard.noIssues')" />
        </section>
      </div>
    </div>
    <el-empty v-else :image-size="52" :description="auth.user ? t('login.waitingForProject') : t('projectDashboard.noProject')" />
    <el-dialog v-model="confirmVisible" :title="confirmTitle" width="min(440px, 92vw)">
      <p v-if="pendingNext" class="confirm-text">{{ t('projectDashboard.confirmSwitchDesc', { label: pendingNext.target.label }) }}</p>
      <el-alert
        v-if="pendingNext?.action === 'complete' && (unresolvedIssues ?? 0) > 0"
        class="confirm-alert"
        type="warning"
        show-icon
        :closable="false"
        :title="t('projectDashboard.unresolvedWarning', { count: unresolvedIssues })"
        :description="t('projectDashboard.unresolvedDesc')"
      />
      <template #footer>
        <el-button @click="confirmVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="transitioning" @click="confirmTransition">{{ t('projectDashboard.confirmSwitch') }}</el-button>
      </template>
    </el-dialog>
    <FormDialog v-model="memberDialog" :title="memberDialogTitle" width="min(460px, 92vw)" :loading="memberSaving" @submit="submitMember">
      <el-form-item :label="t('projectDashboard.memberManagement.userId')">
        <el-select v-if="auth.isAdmin && !editingMember" v-model="memberDraft.user_id" filterable clearable class="full-width" :placeholder="t('projectDashboard.memberManagement.userIdPlaceholder')">
          <el-option v-for="user in availableUsers" :key="user.id" :label="user.display_name || user.username" :value="user.id" />
        </el-select>
        <el-input v-else v-model="memberDraft.user_id" :disabled="!!editingMember" :placeholder="t('projectDashboard.memberManagement.userIdPlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('projectDashboard.memberManagement.projectRole')">
        <el-select v-model="memberDraft.role" class="full-width">
          <el-option v-for="role in memberRoles" :key="role" :label="roleLabel(role)" :value="role" />
        </el-select>
      </el-form-item>
    </FormDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '@/composables/useNotify'
import { formatDateTime } from '@/utils/datetime'
import { useProjectStore } from '@/stores/project'
import { useAuthStore } from '@/stores/auth'
import { addMember, getMembers, removeMember, transitionProject, updateMemberRole, type ProjectMember } from '@/api/projects'
import { listUsers, type UserInfo } from '@/api/auth'
import FormDialog from '@/components/base/FormDialog.vue'
import { listProjectLogs, type LogItem } from '@/api/logs'
import { listProjectIssues, type Issue } from '@/api/issues'
import { resolveLocalizedText } from '@/utils/contentLanguage'
import { logCategoryKey } from '@/utils/logMeta'

const router = useRouter()
const route = useRoute()
const store = useProjectStore()
const auth = useAuthStore()
const { t, locale } = useI18n()

const members = ref<ProjectMember[]>([])
const logs = ref<LogItem[]>([])
function localized(log: LogItem) { return resolveLocalizedText(log.content, log.translations?.content, locale.value) }
// 日志分类走 logMeta 显式 i18n key 映射，未登记值回退原文（log-view-optimization 批：不再显示原始英文枚举）
function categoryLabel(c: string) { const key = logCategoryKey(c); return key ? t(key) : c }
const issues = ref<Issue[]>([])
const issueTotal = ref(0)
const loading = ref(false)
const loadError = ref('')
const projectId = computed(() => String(route.params.id || ''))
const memberKeyword = ref('')
const memberRoleFilter = ref('')
const memberDialog = ref(false)
const memberSaving = ref(false)
const editingMember = ref<ProjectMember | null>(null)
const memberDraft = ref({ user_id: '', role: 'member' })
const adminUsers = ref<UserInfo[]>([])
const memberRoles = ['owner', 'maintainer', 'member', 'viewer']
const currentProjectMember = computed(() => members.value.find((m) => m.user_id === auth.user?.id))
const canManageMembers = computed(() => auth.isAdmin || ['owner', 'maintainer'].includes(currentProjectMember.value?.role || ''))
const ownerCount = computed(() => members.value.filter((m) => m.role === 'owner').length)
const filteredMembers = computed(() => members.value.filter((m) => (!memberKeyword.value || memberName(m).toLowerCase().includes(memberKeyword.value.toLowerCase())) && (!memberRoleFilter.value || m.role === memberRoleFilter.value)))
const availableUsers = computed(() => adminUsers.value.filter((u) => u.role !== 'agent' && !u.disabled && !members.value.some((m) => m.user_id === u.id)))
const memberDialogTitle = computed(() => editingMember.value ? t('projectDashboard.memberManagement.editRole') : t('projectDashboard.memberManagement.add'))

watch(projectId, async (id) => {
  members.value = []
  memberKeyword.value = ''
  memberRoleFilter.value = ''
  memberDialog.value = false
  editingMember.value = null
  logs.value = []
  issues.value = []
  issueTotal.value = 0
  loadError.value = ''
  if (!id) return
  loading.value = true
  try {
    // Three sections load independently: a single API failure shows an error banner without blocking other sections
    const [memberRes, logRes, issueRes] = await Promise.allSettled([
      getMembers(id),
      listProjectLogs(id, { per_page: 5 }),
      listProjectIssues(id, { status: 'open', per_page: 5, sort: 'created', order: 'desc' })
    ])
    if (id !== projectId.value) return
    if (memberRes.status === 'fulfilled') {
      const data = memberRes.value
      // members API returns a bare array; also handle paginated object format as fallback
      members.value = Array.isArray(data) ? data : (data as { items?: ProjectMember[] }).items || []
    }
    // Backend returns items: null for empty lists; normalize to empty array
    if (logRes.status === 'fulfilled') logs.value = logRes.value.items || []
    if (issueRes.status === 'fulfilled') {
      issues.value = issueRes.value.items || []
      issueTotal.value = issueRes.value.total ?? 0
    }
    const failed = [memberRes, logRes, issueRes].find((r) => r.status === 'rejected')
    if (failed && failed.status === 'rejected') {
      loadError.value = failed.reason instanceof Error ? failed.reason.message : t('projectDashboard.loadFailed')
    }
  } finally {
    if (id === projectId.value) loading.value = false
  }
}, { immediate: true })

type StageItem = { key: string; label: string; icon: string }

const STAGES = computed<StageItem[]>(() => [
  { key: 'draft', label: t('project.stages.draft'), icon: '📝' },
  { key: 'active', label: t('project.stages.active'), icon: '🔬' },
  { key: 'completed', label: t('project.stages.completed'), icon: '✅' },
  { key: 'archived', label: t('project.stages.archived'), icon: '📦' }
])

// Maps to valid transitions in go-server projects.targetStatus
const NEXT_ACTIONS = computed<Record<string, { action: string; label: string; target: string }>>(() => ({
  draft: { action: 'activate', label: t('projectDashboard.actionActivate'), target: 'active' },
  active: { action: 'complete', label: t('projectDashboard.actionComplete'), target: 'completed' },
  completed: { action: 'archive', label: t('projectDashboard.actionArchive'), target: 'archived' }
}))

// Backward transitions, admin-only visibility/invocation (backend re-validates)
const BACK_ACTIONS = computed<Record<string, { action: string; label: string; target: string; type: 'info' | 'warning' }>>(() => ({
  active: { action: 'deactivate', label: t('projectDashboard.actionDeactivate'), target: 'draft', type: 'info' as const },
  completed: { action: 'reopen', label: t('projectDashboard.actionReopen'), target: 'active', type: 'warning' as const }
}))
const BACKWARD_ACTIONS = new Set(['deactivate', 'reopen'])

// Stage permission descriptions, consistent with docs/project-design.md §3
const STAGE_DESC = computed<Record<string, { can: string[]; cannot: string[] }>>(() => ({
  draft: {
    can: [t('projectDashboard.descDraftCan1'), t('projectDashboard.descDraftCan2'), t('projectDashboard.descDraftCan3')],
    cannot: [t('projectDashboard.descDraftCannot1')]
  },
  active: {
    can: [t('projectDashboard.descActiveCan1'), t('projectDashboard.descActiveCan2'), t('projectDashboard.descActiveCan3')],
    cannot: []
  },
  completed: {
    can: [t('projectDashboard.descCompletedCan1'), t('projectDashboard.descCompletedCan2')],
    cannot: [t('projectDashboard.descCompletedCannot1')]
  },
  archived: {
    can: [t('projectDashboard.descArchivedCan1'), t('projectDashboard.descArchivedCan2')],
    cannot: [t('projectDashboard.descArchivedCannot1')]
  }
}))

const currentIndex = computed(() => {
  const index = STAGES.value.findIndex((stage) => stage.key === store.current?.status)
  return index >= 0 ? index : 0
})
const currentStage = computed(() => STAGES.value[currentIndex.value])
const stageDesc = computed(() => STAGE_DESC.value[currentStage.value.key])
const nextAction = computed(() => (store.current ? NEXT_ACTIONS.value[store.current.status] : undefined))
const backAction = computed(() => (store.current && auth.isAdmin ? BACK_ACTIONS.value[store.current.status] : undefined))
const nodeState = (index: number) => (index < currentIndex.value ? 'done' : index === currentIndex.value ? 'current' : 'future')

const confirmVisible = ref(false)
const transitioning = ref(false)
const unresolvedIssues = ref<number | null>(null)
const pendingNext = ref<{ action: string; target: StageItem; from: StageItem } | null>(null)
const confirmTitle = computed(() => {
  const pending = pendingNext.value
  if (!pending) return t('projectDashboard.confirmTitleDefault')
  if (BACKWARD_ACTIONS.has(pending.action)) {
    return t('projectDashboard.confirmBackTemplate', { from: pending.from.label, to: pending.target.label })
  }
  return t('projectDashboard.confirmForwardTemplate', { from: pending.from.label, to: pending.target.label })
})

function openConfirm() {
  openTransition(nextAction.value)
}

function openBackConfirm() {
  openTransition(backAction.value)
}

function openTransition(action?: { action: string; target: string }) {
  if (!store.current || !action) return
  const target = STAGES.value.find((stage) => stage.key === action.target)
  if (!target) return
  pendingNext.value = { action: action.action, target, from: currentStage.value }
  unresolvedIssues.value = null
  confirmVisible.value = true
  // Fetch unresolved issue count before marking complete, for dialog warning
  if (action.action === 'complete') void loadUnresolvedIssues()
}

async function loadUnresolvedIssues() {
  const id = store.current?.id
  if (!id) return
  try {
    const [openRes, progressRes] = await Promise.all([
      listProjectIssues(id, { status: 'open', per_page: 1 }),
      listProjectIssues(id, { status: 'in_progress', per_page: 1 })
    ])
    if (id !== projectId.value || !confirmVisible.value) return
    unresolvedIssues.value = (openRes.total || 0) + (progressRes.total || 0)
  } catch {
    // If fetch fails, skip count warning; backend still validates the transition
  }
}

async function confirmTransition() {
  const pending = pendingNext.value
  if (!store.current || !pending) return
  transitioning.value = true
  try {
    const payload: { action: string; ignore_warnings?: boolean } = { action: pending.action }
    // Dialog already showed the unresolved issue warning and user confirmed; skip duplicate backend alert
    if (pending.action === 'complete') payload.ignore_warnings = true
    await transitionProject(store.current.id, payload)
    ElMessage.success(t('projectDashboard.switchedTo', { label: pending.target.label }))
    confirmVisible.value = false
    await store.load()
  } catch (err) {
    showApiError(err, t('projectDashboard.transitionFailed'))
  } finally {
    transitioning.value = false
  }
}

const roleLabel = (role: string) => {
  const map: Record<string, string> = {
    owner: t('projectDashboard.roleOwner'),
    maintainer: t('projectDashboard.roleMaintainer'),
    member: t('projectDashboard.roleMember'),
    viewer: t('projectDashboard.roleViewer')
  }
  return map[role] || role
}
function memberName(member: ProjectMember) {
  const user = adminUsers.value.find((u) => u.id === member.user_id)
  return user?.display_name || user?.username || (member as ProjectMember & { username?: string }).username || member.user_id
}
function isLastOwner(member: ProjectMember) { return member.role === 'owner' && ownerCount.value === 1 }
async function openAddMember() {
  editingMember.value = null
  memberDraft.value = { user_id: '', role: 'member' }
  if (auth.isAdmin && !adminUsers.value.length) {
    try { adminUsers.value = (await listUsers()).filter((u) => u.role !== 'agent') } catch (err) { showApiError(err, t('projectDashboard.memberManagement.loadFailed')) }
  }
  memberDialog.value = true
}
function openEditMember(member: ProjectMember) {
  editingMember.value = member
  memberDraft.value = { user_id: member.user_id, role: member.role }
  memberDialog.value = true
}
async function submitMember() {
  const id = projectId.value
  if (!id || !memberDraft.value.user_id || !memberDraft.value.role || (editingMember.value && memberDraft.value.role === editingMember.value.role)) return
  memberSaving.value = true
  try {
    const result = editingMember.value ? await updateMemberRole(id, memberDraft.value.user_id, memberDraft.value.role) : await addMember(id, memberDraft.value)
    ElMessage.success(t(editingMember.value ? 'projectDashboard.memberManagement.updateSuccess' : 'projectDashboard.memberManagement.addSuccess', { requestId: result.requestId }))
    memberDialog.value = false
    members.value = await getMembers(id)
  } catch (err) { showApiError(err, t('projectDashboard.memberManagement.loadFailed')) } finally { memberSaving.value = false }
}
async function remove(member: ProjectMember) {
  if (isLastOwner(member)) return
  try { await ElMessageBox.confirm(t('projectDashboard.memberManagement.removeConfirm', { user: memberName(member), project: store.current?.name || '' }), t('projectDashboard.memberManagement.remove')) } catch { return }
  try {
    const result = await removeMember(projectId.value, member.user_id)
    ElMessage.success(t('projectDashboard.memberManagement.removeSuccess', { requestId: result.requestId }))
    members.value = await getMembers(projectId.value)
    if (member.user_id === auth.user?.id) { await store.load(); await router.push('/projects') }
  } catch (err) { showApiError(err, t('projectDashboard.memberManagement.loadFailed')) }
}
const severityLabel = (severity: string) => {
  const map: Record<string, string> = {
    low: t('projectDashboard.severityLow'),
    medium: t('projectDashboard.severityMedium'),
    high: t('projectDashboard.severityHigh'),
    critical: t('projectDashboard.severityCritical')
  }
  return map[severity] || severity
}
const go = (path: string) => router.push(path)
</script>

<style scoped>
.dashboard {
  align-content: start;
  display: grid;
  min-height: 320px;
}

.member-filters { display: flex; gap: var(--space-2); margin-bottom: var(--space-3); }
.member-filters .el-input { max-width: 260px; }
.member-filters .el-select { max-width: 180px; }
.member-actions { margin-left: auto; }
.full-width { width: 100%; }

.dash-title {
  display: grid;
  gap: 6px;
}

.stage-panel {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: 14px;
  padding: 16px 18px;
}

.stage-flow {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px 6px;
}

.stage-node {
  align-items: center;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-full);
  color: var(--text-3);
  display: flex;
  gap: 8px;
  min-height: 38px;
  padding: 6px 14px;
}

.stage-badge {
  font-size: 15px;
  line-height: 1;
}

.stage-name {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
}

.stage-node[data-state='done'] {
  background: var(--ok-soft);
  border-color: var(--ok);
  color: var(--ok);
}

.stage-node[data-state='done'] .stage-badge {
  align-items: center;
  background: var(--ok);
  border-radius: 50%;
  color: var(--text-inverse);
  display: inline-flex;
  font-size: 11px;
  height: 18px;
  justify-content: center;
  width: 18px;
}

.stage-node[data-state='current'] {
  background: var(--brand-050);
  border-color: var(--brand-600);
  box-shadow: 0 0 0 3px var(--brand-100);
  color: var(--brand-700);
}

.stage-back {
  margin-left: 4px;
}

.back-arrow {
  margin-right: 2px;
}

.stage-next {
  margin-left: 4px;
}

.next-arrow {
  margin-left: 2px;
}

.stage-arrow {
  color: var(--border-strong);
  font-size: 16px;
}

.stage-arrow[data-done='true'] {
  color: var(--ok);
}

.stage-desc {
  border-top: 1px dashed var(--border);
  display: grid;
  gap: 4px;
  padding-top: 12px;
}

.stage-desc-title {
  color: var(--text-1);
  font-size: 13px;
}

.desc-line {
  color: var(--text-2);
  font-size: 13px;
}

.confirm-text {
  color: var(--text-2);
}

.confirm-alert {
  margin-top: 12px;
}

@media (max-width: 768px) {
  .stage-node {
    padding: 5px 10px;
  }
}

.metric-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
}

.metric {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: 2px;
  overflow: hidden;
  padding: 18px 18px 18px 21px;
  position: relative;
  transition:
    box-shadow 0.15s ease,
    transform 0.15s ease;
}

.metric::before {
  background: linear-gradient(180deg, var(--brand-500), var(--brand-700));
  border-radius: 3px;
  bottom: 14px;
  content: '';
  left: 0;
  position: absolute;
  top: 14px;
  width: 3px;
}

.metric:hover {
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.metric strong {
  color: var(--brand-700);
  font-size: 30px;
  font-weight: 700;
  line-height: 1.1;
}

.metric span {
  color: var(--text-3);
  font-size: 13px;
}

.overview-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  min-height: 180px;
}

.overview-card {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: var(--space-3);
  padding: 16px;
}

.timeline-card {
  grid-row: span 2;
}

.overview-head h3 {
  font-size: 15px;
}

.overview-head-actions {
  align-items: center;
  display: flex;
  gap: 4px;
}

.member-list,
.issue-list,
.timeline-list {
  align-content: start;
  display: grid;
}

.member-row,
.issue-row {
  align-items: center;
  border-top: 1px solid var(--border);
  display: flex;
  gap: var(--space-3);
  justify-content: space-between;
  min-width: 0;
  padding: 10px 0;
}

.member-row:first-child,
.issue-row:first-child {
  border-top: 0;
}

.member-row > span,
.issue-row > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.timeline-item {
  border-left: 2px solid var(--brand-100);
  display: grid;
  gap: 6px;
  padding: 0 0 16px 14px;
}

.timeline-item:last-child {
  padding-bottom: 0;
}

.timeline-meta {
  align-items: center;
  display: flex;
  gap: 10px;
  justify-content: space-between;
}

.timeline-meta time {
  color: var(--text-3);
  font-size: 12px;
}

.timeline-item p {
  color: var(--text-2);
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.severity-tag[data-severity='low'] { --el-tag-bg-color: var(--info); --el-tag-border-color: var(--info); }
.severity-tag[data-severity='medium'] { --el-tag-bg-color: var(--warn); --el-tag-border-color: var(--warn); }
.severity-tag[data-severity='high'] { --el-tag-bg-color: var(--danger); --el-tag-border-color: var(--danger); }
.severity-tag[data-severity='critical'] { --el-tag-bg-color: var(--danger); --el-tag-border-color: var(--danger); }

@media (max-width: 768px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }

  .timeline-card {
    grid-row: auto;
  }
}
</style>
