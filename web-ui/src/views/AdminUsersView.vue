<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('adminUsers.title') }}</h2>
      <div class="toolbar-actions">
        <el-input
          v-model="keyword"
          class="search-input"
          :placeholder="t('adminUsers.searchPlaceholder')"
          clearable
          :prefix-icon="Search"
        />
        <el-button @click="load">{{ t('adminUsers.refresh') }}</el-button>
        <el-button type="primary" @click="createDialog = true">{{ t('adminUsers.create') }}</el-button>
      </div>
    </div>
    <section class="panel">
      <el-alert
        v-if="loadError"
        class="load-error"
        type="error"
        :title="loadError"
        show-icon
        :closable="false"
      >
        <el-button size="small" @click="load">{{ t('adminUsers.retry') }}</el-button>
      </el-alert>
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
    </section>

    <el-dialog v-model="roleDialog" :title="t('adminUsers.changeRoleTitle')" width="440">
      <p v-if="roleTarget" class="role-dialog-text">
        <span v-html="t('adminUsers.changeRoleFrom', { username: roleTarget.username, role: roleLabel(roleTarget.role) })" />
      </p>
      <el-select v-model="roleDraft" class="role-select">
        <el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" />
      </el-select>
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
    </el-dialog>

    <el-dialog v-model="passwordDialog" :title="t('adminUsers.tempPassword')" width="460">
      <div class="password-row">
        <el-input v-model="temporaryPassword" readonly />
        <el-button type="primary" @click="copyPassword">{{ t('adminUsers.copy') }}</el-button>
      </div>
      <p class="muted dialog-hint">{{ t('adminUsers.tempPasswordHint') }}</p>
    </el-dialog>

    <el-dialog v-model="createDialog" :title="t('adminUsers.createUser')" width="520">
      <el-form label-position="top">
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
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="create">{{ t('adminUsers.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import { Search } from '@element-plus/icons-vue'
import { createUser, listUsers, resetPassword, updateUser, type UserInfo } from '../api/auth'
import { useAuthStore } from '../stores/auth'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'

const { t } = useI18n()
const auth = useAuthStore()
const users = ref<UserInfo[]>([])
const keyword = ref('')
const loading = ref(false)
const loadError = ref('')
const saving = ref(false)

// Agent accounts are for internal system use; they are not shown on the user management page nor assignable.
const roles = ['admin', 'maintainer', 'member', 'viewer']

const passwordDialog = ref(false)
const createDialog = ref(false)
const temporaryPassword = ref('')
const roleDialog = ref(false)
const roleTarget = ref<UserInfo | null>(null)
const roleDraft = ref('member')
const draft = reactive({ username: '', display_name: '', role: 'member', password: '' })

onMounted(load)

const filteredUsers = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return users.value
  return users.value.filter(
    (u) => u.username.toLowerCase().includes(kw) || (u.display_name || '').toLowerCase().includes(kw)
  )
})

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

function formatDate(iso: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function openRoleDialog(row: UserInfo) {
  roleTarget.value = row
  roleDraft.value = row.role
  roleDialog.value = true
}

async function confirmRoleChange() {
  if (!roleTarget.value) return
  saving.value = true
  try {
    await updateUser(roleTarget.value.id, { role: roleDraft.value })
    ElMessage.success(t('adminUsers.roleUpdated'))
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
    temporaryPassword.value = data.temporary_password
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
    const data = await createUser({
      username: draft.username,
      display_name: draft.display_name,
      role: draft.role,
      password: draft.password || undefined
    })
    temporaryPassword.value = data.temporary_password
    passwordDialog.value = true
    createDialog.value = false
    draft.username = ''
    draft.display_name = ''
    draft.role = 'member'
    draft.password = ''
    await load()
  } catch (err) {
    showApiError(err, t('adminUsers.createFailed'))
  } finally {
    saving.value = false
  }
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
.toolbar-actions {
  display: flex;
  gap: 10px;
}

.search-input {
  width: 240px;
}

.load-error {
  margin-bottom: 12px;
}

.user-cell {
  align-items: center;
  display: flex;
  gap: 12px;
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
