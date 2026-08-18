<template>
  <!-- 列表骨架统一 base/ListPage（结构改版 R3）：搜索在 actions 槽；列表错误态收口 StateBlock -->
  <ListPage
    :title="t('adminUsers.title')"
    :error="loadError ? { message: loadError } : null"
    @retry="load"
  >
    <template #actions>
      <el-input
        v-model="keyword"
        class="search-input"
        :placeholder="t('adminUsers.searchPlaceholder')"
        clearable
        :prefix-icon="Search"
      />
      <el-select v-model="roleFilter" clearable :placeholder="t('adminUsers.filterRole')"><el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" /></el-select>
      <el-select v-model="statusFilter" clearable :placeholder="t('adminUsers.filterStatus')"><el-option value="active" :label="t('adminUsers.active')" /><el-option value="disabled" :label="t('adminUsers.disabled')" /></el-select>
      <el-button v-if="roleFilter || statusFilter || keyword" link @click="clearFilters">{{ t('adminUsers.clearFilters') }}</el-button>
      <el-button @click="load">{{ t('adminUsers.refresh') }}</el-button>
      <el-button type="primary" @click="createDialog = true">{{ t('adminUsers.create') }}</el-button>
    </template>
    <div class="summary-grid"><div>{{ t('adminUsers.summaryTotal') }} <strong>{{ users.length }}</strong></div><div>{{ t('adminUsers.summaryActive') }} <strong>{{ activeCount }}</strong></div><div>{{ t('adminUsers.summaryMustChangePassword') }} <strong>{{ mustChangeCount }}</strong></div><div>{{ t('adminUsers.summaryDisabled') }} <strong>{{ disabledCount }}</strong></div></div>
    <ResponsiveTable :rows="filteredUsers" :loading="loading">
      <el-table-column :label="t('adminUsers.tableUser')" min-width="220">
        <template #default="{ row }">
          <div class="user-cell">
            <el-avatar :size="36">{{ avatarText(row) }}</el-avatar>
            <div class="user-meta">
              <span class="username">{{ row.username }}</span>
              <span class="muted">{{ row.display_name || t('adminUsers.noDisplayName') }}</span>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('adminUsers.tableRole')" width="110">
        <template #default="{ row }">
          <el-tag :type="roleTagType(row.role)" effect="light">{{ roleLabel(row.role) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('adminUsers.tableJoinTime')" width="120">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('adminUsers.tableStatus')" width="90">
        <template #default="{ row }">
          <el-tag :type="row.disabled ? 'danger' : 'success'" effect="light">
            {{ row.disabled ? t('adminUsers.disabled') : t('adminUsers.active') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('adminUsers.tableActions')" width="280">
        <template #default="{ row }">
          <template v-if="row.id !== auth.user?.id">
            <el-button size="small" @click="openRoleDialog(row)">{{ t('adminUsers.changeRole') }}</el-button>
            <el-button size="small" @click="openJoinDialog(row)">{{ t('adminUsers.joinProject') }}</el-button>
            <el-button size="small" @click="reset(row)">{{ t('adminUsers.resetPassword') }}</el-button>
            <el-button
              size="small"
              :type="row.disabled ? 'success' : 'danger'"
              plain
              @click="toggleDisabled(row)"
            >
              {{ row.disabled ? t('adminUsers.enable') : t('adminUsers.disable') }}
            </el-button>
          </template>
          <span v-else class="muted">{{ t('adminUsers.currentAccount') }}</span>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="keyword ? t('adminUsers.noMatch') : t('adminUsers.empty')" />
      </template>
      <template #card="{ row }">
        <div class="user-card">
          <div class="user-card-title">
            <el-avatar :size="32">{{ avatarText(row) }}</el-avatar>
            <span class="card-title">{{ row.username }}</span>
          </div>
          <div class="card-fields">
            <span>{{ row.display_name || t('adminUsers.noDisplayName') }}</span>
            <el-tag :type="roleTagType(row.role)" size="small" effect="light">{{ roleLabel(row.role) }}</el-tag>
            <el-tag :type="row.disabled ? 'danger' : 'success'" size="small" effect="light">
              {{ row.disabled ? t('adminUsers.disabled') : t('adminUsers.active') }}
            </el-tag>
          </div>
          <div class="card-actions">
            <template v-if="row.id !== auth.user?.id">
              <el-button size="small" @click="openRoleDialog(row)">{{ t('adminUsers.changeRole') }}</el-button>
              <el-button size="small" @click="openJoinDialog(row)">{{ t('adminUsers.joinProject') }}</el-button>
              <el-button size="small" @click="reset(row)">{{ t('adminUsers.resetPassword') }}</el-button>
              <el-button
                size="small"
                :type="row.disabled ? 'success' : 'danger'"
                plain
                @click="toggleDisabled(row)"
              >
                {{ row.disabled ? t('adminUsers.enable') : t('adminUsers.disable') }}
              </el-button>
            </template>
            <span v-else class="muted">{{ t('adminUsers.currentAccount') }}</span>
          </div>
        </div>
      </template>
    </ResponsiveTable>
  </ListPage>

  <section class="admin-section">
    <div class="section-heading"><h2>{{ t('adminUsers.invitationCodes.title') }}</h2><el-button type="primary" @click="invitationDialog = true">{{ t('adminUsers.invitationCodes.generate') }}</el-button></div>
    <div class="invitation-filters"><el-select v-model="invitationStatus" clearable><el-option value="active" :label="t('adminUsers.invitationCodes.statusActive')" /><el-option value="used" :label="t('adminUsers.invitationCodes.statusUsed')" /><el-option value="expired" :label="t('adminUsers.invitationCodes.statusExpired')" /><el-option value="revoked" :label="t('adminUsers.invitationCodes.statusRevoked')" /></el-select><el-button @click="loadInvitations">{{ t('adminUsers.refresh') }}</el-button></div>
    <StateBlock v-if="invitationError" :error="{ message: invitationError }" @retry="loadInvitations" />
    <el-empty v-else-if="!invitations.length && !invitationLoading" :description="t('adminUsers.invitationCodes.empty')" />
    <ResponsiveTable v-else :rows="invitations" :loading="invitationLoading"><el-table-column :label="t('adminUsers.invitationCodes.codePrefix')" prop="code_prefix" /><el-table-column :label="t('adminUsers.invitationCodes.status')"><template #default="{row}"><StatusBadge domain="invitationCode" :value="row.status" /></template></el-table-column><el-table-column :label="t('adminUsers.invitationCodes.expiresAt')"><template #default="{row}">{{ formatDate(row.expires_at) }}</template></el-table-column><el-table-column :label="t('adminUsers.invitationCodes.usedBy')" prop="used_by" /><el-table-column width="100"><template #default="{row}"><el-button v-if="row.status === 'active'" size="small" type="danger" @click="revokeInvitation(row)">{{ t('adminUsers.invitationCodes.revoke') }}</el-button></template></el-table-column></ResponsiveTable>
  </section>
  <FormDialog v-model="invitationDialog" :title="t('adminUsers.invitationCodes.generateTitle')" :loading="invitationSaving" @submit="generateInvitation"><el-form-item :label="t('adminUsers.invitationCodes.expiresAt')"><el-input v-model="invitationExpiry" type="datetime-local" /><small class="muted">{{ t('adminUsers.invitationCodes.defaultExpiryHint') }}</small></el-form-item></FormDialog>
  <el-dialog v-model="invitationResultDialog" :title="t('adminUsers.invitationCodes.generateTitle')" width="480"><el-input v-model="invitationCode" readonly /><p class="muted">{{ t('adminUsers.invitationCodes.oneTimeWarning') }}</p><p>{{ t('adminUsers.invitationCodes.generateSuccess', { requestId: invitationRequestId }) }}</p><template #footer><el-button type="primary" @click="copyInvitation">{{ t('adminUsers.invitationCodes.copy') }}</el-button><el-button @click="closeInvitationResult">{{ t('common.confirm') }}</el-button></template></el-dialog>

    <FormDialog v-model="roleDialog" :title="t('adminUsers.changeRoleTitle')" width="440">
      <p v-if="roleTarget" class="role-dialog-text">
        <span>{{ t('adminUsers.changeRoleFrom', { username: roleTarget.username, role: roleLabel(roleTarget.role) }) }}</span>
      </p>
      <el-select v-model="roleDraft" class="role-select">
        <el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" />
      </el-select>
      <el-form-item :label="t('adminUsers.formDisplayName')"><el-input v-model="editDisplayName" /></el-form-item>
      <el-checkbox v-model="editDisabled">{{ t('adminUsers.disabled') }}</el-checkbox>
      <template #footer>
        <el-button @click="roleDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button
          type="primary"
          :disabled="!roleTarget || roleDraft === roleTarget.role"
          :loading="saving"
          @click="confirmRoleChange"
        >
          {{ t('adminUsers.confirmChange') }}
        </el-button>
      </template>
    </FormDialog>

    <el-dialog v-model="passwordDialog" :title="t('adminUsers.tempPassword')" width="460">
      <div class="password-row">
        <el-input v-model="temporaryPassword" readonly />
        <el-button type="primary" @click="copyPassword">{{ t('adminUsers.copy') }}</el-button>
      </div>
      <p class="muted dialog-hint">{{ t('adminUsers.tempPasswordHint') }}</p>
    </el-dialog>

    <FormDialog v-model="createDialog" :title="t('adminUsers.createUser')" width="520" :loading="saving" @submit="create">
      <el-form-item :label="t('adminUsers.formUsername')"><el-input v-model="draft.username" /></el-form-item>
      <el-form-item :label="t('adminUsers.formDisplayName')"><el-input v-model="draft.display_name" /></el-form-item>
      <el-form-item :label="t('adminUsers.formRole')">
        <el-select v-model="draft.role">
          <el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('adminUsers.formPassword')">
        <el-input v-model="draft.password" type="password" show-password :placeholder="t('adminUsers.passwordPlaceholder')" />
      </el-form-item>
      <el-checkbox v-model="draft.join">{{ t('adminUsers.createAndJoin') }}</el-checkbox>
      <template v-if="draft.join"><el-form-item :label="t('adminUsers.project')"><el-select v-model="draft.project_id" class="role-select"><el-option v-for="project in projectStore.projects" :key="project.id" :label="project.name" :value="project.id" /></el-select></el-form-item><el-form-item :label="t('adminUsers.projectRole')"><el-select v-model="draft.project_role" class="role-select"><el-option v-for="role in projectRoles" :key="role" :label="roleLabel(role)" :value="role" /></el-select></el-form-item></template>
    </FormDialog>
    <FormDialog v-model="joinDialog" :title="t('adminUsers.joinProject')" width="460" :loading="saving" @submit="joinProject">
      <p v-if="joinTarget">{{ joinTarget.username }}</p>
      <el-form-item :label="t('adminUsers.project')"><el-select v-model="joinDraft.project_id" class="role-select"><el-option v-for="project in projectStore.projects" :key="project.id" :label="project.name" :value="project.id" /></el-select></el-form-item>
      <el-form-item :label="t('adminUsers.projectRole')"><el-select v-model="joinDraft.role" class="role-select"><el-option v-for="role in projectRoles" :key="role" :label="roleLabel(role)" :value="role" /></el-select></el-form-item>
    </FormDialog>
    <el-dialog v-model="resultDialog" :title="t('adminUsers.accountCreated')" width="520">
      <p v-if="createResult.joinFailed">{{ t('adminUsers.joinFailedAfterCreate') }}</p><p>{{ t('adminUsers.createRequestId') }}: {{ createResult.createRequestId }}</p><p v-if="createResult.joinRequestId">{{ t('adminUsers.joinRequestId') }}: {{ createResult.joinRequestId }}</p><p>{{ t('adminUsers.tempPassword') }}: {{ temporaryPassword }}</p>
      <template #footer><el-button v-if="createResult.joinFailed" type="primary" @click="retryJoin">{{ t('adminUsers.retryJoin') }}</el-button><el-button @click="resultDialog = false">{{ t('common.confirm') }}</el-button></template>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import { Search } from '@element-plus/icons-vue'
import { createUser, listUsers, resetPassword, updateUser, listInvitationCodes, createInvitationCode, revokeInvitationCode, type UserInfo, type InvitationCode } from '../api/auth'
import { useAuthStore } from '../stores/auth'
import { useProjectStore } from '../stores/project'
import { addMember } from '../api/projects'
import ListPage from '@/components/base/ListPage.vue'
import FormDialog from '@/components/base/FormDialog.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { formatDate } from '@/utils/datetime'
import StateBlock from '@/components/base/StateBlock.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'

const { t } = useI18n()
const auth = useAuthStore()
const projectStore = useProjectStore()
const users = ref<UserInfo[]>([])
const keyword = ref('')
const loading = ref(false)
const loadError = ref('')
const saving = ref(false)
const roleFilter = ref('')
const statusFilter = ref('')
const joinDialog = ref(false)
const joinTarget = ref<UserInfo | null>(null)
const joinDraft = reactive({ project_id: '', role: 'member' })
const projectRoles = ['owner', 'maintainer', 'member', 'viewer']
const createResult = reactive({ userId: '', createRequestId: '', joinRequestId: '', joinFailed: false, projectId: '', projectRole: 'member' })

// Agent accounts are for internal system use; they are not shown on the user management page nor assignable.
const roles = ['admin', 'maintainer', 'member', 'viewer']

const passwordDialog = ref(false)
const resultDialog = ref(false)
const createDialog = ref(false)
const temporaryPassword = ref('')
const roleDialog = ref(false)
const roleTarget = ref<UserInfo | null>(null)
const roleDraft = ref('member')
const editDisplayName = ref('')
const editDisabled = ref(false)
const draft = reactive({ username: '', display_name: '', role: 'member', password: '', join: false, project_id: '', project_role: 'member' })
const invitations = ref<InvitationCode[]>([]); const invitationStatus = ref(''); const invitationLoading = ref(false); const invitationError = ref(''); const invitationDialog = ref(false); const invitationSaving = ref(false); const invitationExpiry = ref(''); const invitationResultDialog = ref(false); const invitationCode = ref(''); const invitationRequestId = ref('')

onMounted(() => { load(); loadInvitations() })

async function loadInvitations() { invitationLoading.value=true; invitationError.value=''; try { invitations.value=(await listInvitationCodes(invitationStatus.value ? {status: invitationStatus.value} : {})).items } catch(err) { invitationError.value=err instanceof Error?err.message:t('adminUsers.invitationCodes.loadFailed') } finally { invitationLoading.value=false } }
async function generateInvitation() { invitationSaving.value=true; try { const result=await createInvitationCode(invitationExpiry.value ? {expires_at:new Date(invitationExpiry.value).toISOString()} : {}); invitationCode.value=result.data.code; invitationRequestId.value=result.requestId; invitationDialog.value=false; invitationResultDialog.value=true; invitationExpiry.value=''; await loadInvitations() } catch(err) { showApiError(err,t('adminUsers.invitationCodes.generateFailed')) } finally { invitationSaving.value=false } }
async function copyInvitation() { try { if(navigator.clipboard?.writeText) await navigator.clipboard.writeText(invitationCode.value); else { const el=document.createElement('textarea');el.value=invitationCode.value;document.body.appendChild(el);el.select();document.execCommand('copy');el.remove() }; ElMessage.success(t('adminUsers.invitationCodes.copied')) } catch { ElMessage.error(t('adminUsers.copyFailed')) } }
async function closeInvitationResult() { invitationResultDialog.value=false; invitationCode.value='' }
async function revokeInvitation(row: InvitationCode) { try { await ElMessageBox.confirm(t('adminUsers.invitationCodes.revokeConfirm',{prefix:row.code_prefix}),t('adminUsers.invitationCodes.revoke')); const result=await revokeInvitationCode(row.id);ElMessage.success(t('adminUsers.invitationCodes.revokeSuccess',{requestId:result.requestId}));await loadInvitations() } catch(err) { if(err==='cancel')return;showApiError(err,t('adminUsers.invitationCodes.revokeFailed')) } }

const filteredUsers = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  return users.value.filter(
    (u) => u.username.toLowerCase().includes(kw) || (u.display_name || '').toLowerCase().includes(kw)
  ).filter((u) => !roleFilter.value || u.role === roleFilter.value).filter((u) => !statusFilter.value || (statusFilter.value === 'disabled') === u.disabled)
})
const activeCount = computed(() => users.value.filter((u) => !u.disabled).length)
const disabledCount = computed(() => users.value.filter((u) => u.disabled).length)
const mustChangeCount = computed(() => users.value.filter((u) => u.must_change_password).length)
function clearFilters() { keyword.value = ''; roleFilter.value = ''; statusFilter.value = '' }

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const list = await listUsers()
    users.value = list.filter((u) => u.role !== 'agent')
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : t('adminUsers.loadFailed')
  } finally {
    loading.value = false
  }
}

function roleLabel(role?: string) {
  const map: Record<string, string> = {
    admin: t('adminUsers.roleAdmin'),
    maintainer: t('adminUsers.roleMaintainer'),
    member: t('adminUsers.roleMember'),
    viewer: t('adminUsers.roleViewer')
  }
  return (role && map[role]) || role || '—'
}

function roleTagType(role: string): 'primary' | 'warning' | 'info' {
  if (role === 'admin') return 'warning'
  if (role === 'maintainer') return 'primary'
  return 'info'
}

function avatarText(user: UserInfo) {
  return (user.display_name || user.username).slice(0, 1).toUpperCase()
}


function openRoleDialog(row: UserInfo) {
  roleTarget.value = row
  roleDraft.value = row.role
  editDisplayName.value = row.display_name
  editDisabled.value = row.disabled
  roleDialog.value = true
}

function openJoinDialog(row: UserInfo) {
  joinTarget.value = row
  joinDraft.project_id = projectStore.current?.id || projectStore.projects[0]?.id || ''
  joinDraft.role = 'member'
  joinDialog.value = true
}

async function joinProject() {
  if (!joinTarget.value || !joinDraft.project_id) return
  saving.value = true
  try {
    const result = await addMember(joinDraft.project_id, { user_id: joinTarget.value.id, role: joinDraft.role })
    ElMessage.success(t('adminUsers.joinSuccess', { requestId: result.requestId }))
    joinDialog.value = false
    await load()
  } catch (err) { showApiError(err, t('adminUsers.joinProject')) } finally { saving.value = false }
}

async function confirmRoleChange() {
  if (!roleTarget.value) return
  saving.value = true
  try {
    const result = await updateUser(roleTarget.value.id, { role: roleDraft.value, display_name: editDisplayName.value, disabled: editDisabled.value })
    ElMessage.success(t('adminUsers.roleUpdated', { requestId: result.requestId }))
    roleDialog.value = false
    await load()
  } catch (err) {
    showApiError(err, t('adminUsers.roleUpdateFailed'))
  } finally {
    saving.value = false
  }
}

async function reset(row: UserInfo) {
  try {
    await ElMessageBox.confirm(t('adminUsers.confirmResetPwdMsg', { username: row.username }), t('adminUsers.confirmResetPwdTitle'), {
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    const data = await resetPassword(row.id)
    temporaryPassword.value = data.data.temporary_password
    ElMessage.success(t('adminUsers.resetPasswordSuccess', { requestId: data.requestId }))
    passwordDialog.value = true
    await load()
  } catch (err) {
    showApiError(err, t('adminUsers.resetPwdFailed'))
  }
}

async function toggleDisabled(row: UserInfo) {
  const actionLabel = row.disabled ? t('adminUsers.enable') : t('adminUsers.disable')
  const warning = row.disabled ? '' : t('adminUsers.disableWarning')
  try {
    await ElMessageBox.confirm(
      t('adminUsers.confirmToggleMsg', { action: actionLabel, username: row.username, warning }),
      t('adminUsers.confirmToggleTitle', { action: actionLabel }),
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await updateUser(row.id, { disabled: !row.disabled })
    ElMessage.success(t('adminUsers.toggleSuccess', { action: actionLabel }))
    await load()
  } catch (err) {
    showApiError(err, t('adminUsers.toggleFailed', { action: actionLabel }))
  }
}

async function create() {
  saving.value = true
  try {
    const result = await createUser({
      username: draft.username,
      display_name: draft.display_name,
      role: draft.role,
      password: draft.password || undefined
    })
    temporaryPassword.value = result.data.temporary_password
    createResult.userId = result.data.user.id
    createResult.createRequestId = result.requestId
    createResult.joinRequestId = ''
    createResult.joinFailed = false
    createResult.projectId = draft.project_id
    createResult.projectRole = draft.project_role
    if (draft.join && draft.project_id) {
      try { createResult.joinRequestId = (await addMember(draft.project_id, { user_id: result.data.user.id, role: draft.project_role })).requestId } catch { createResult.joinFailed = true }
    }
    passwordDialog.value = true
    resultDialog.value = true
    createDialog.value = false
    draft.username = ''
    draft.display_name = ''
    draft.role = 'member'
    draft.password = ''
    draft.join = false
    draft.project_id = ''
    draft.project_role = 'member'
    await load()
  } catch (err) {
    showApiError(err, t('adminUsers.createFailed'))
  } finally {
    saving.value = false
  }
}

async function retryJoin() {
  if (!createResult.userId || !createResult.projectId) return
  try {
    createResult.joinRequestId = (await addMember(createResult.projectId, { user_id: createResult.userId, role: createResult.projectRole })).requestId
    createResult.joinFailed = false
  } catch (err) { showApiError(err, t('adminUsers.joinProject')) }
}

async function copyPassword() {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(temporaryPassword.value)
    } else {
      // HTTP intranet deployment has no clipboard API; fall back to a hidden textarea.
      const el = document.createElement('textarea')
      el.value = temporaryPassword.value
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
    }
    ElMessage.success(t('adminUsers.copied'))
  } catch {
    ElMessage.error(t('adminUsers.copyFailed'))
  }
}
</script>

<style scoped>
.search-input {
  width: 240px;
}

.user-cell {
  align-items: center;
  display: flex;
  gap: var(--space-3);
}

.user-meta {
  display: flex;
  flex-direction: column;
  line-height: 1.4;
}

.username {
  font-weight: 600;
}

.user-card-title {
  align-items: center;
  display: flex;
  gap: 10px;
  min-width: 0;
}

.role-dialog-text {
  margin-top: 0;
}

.role-select {
  width: 100%;
}

.password-row {
  display: flex;
  gap: 10px;
}

.dialog-hint {
  font-size: 13px;
  margin-top: 12px;
}
</style>
