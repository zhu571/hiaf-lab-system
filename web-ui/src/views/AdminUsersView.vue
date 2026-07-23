<template>
  <div class="page">
    <div class="toolbar">
      <h2>用户管理</h2>
      <div class="toolbar-actions">
        <el-input
          v-model="keyword"
          class="search-input"
          placeholder="搜索用户名或显示名"
          clearable
          :prefix-icon="Search"
        />
        <el-button @click="load">刷新</el-button>
        <el-button type="primary" @click="createDialog = true">新建用户</el-button>
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
        <el-button size="small" @click="load">重试</el-button>
      </el-alert>
      <el-table v-loading="loading" :data="filteredUsers">
        <el-table-column label="用户" min-width="220">
          <template #default="{ row }">
            <div class="user-cell">
              <el-avatar :size="36">{{ avatarText(row) }}</el-avatar>
              <div class="user-meta">
                <span class="username">{{ row.username }}</span>
                <span class="muted">{{ row.display_name || '未设置显示名' }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="角色" width="110">
          <template #default="{ row }">
            <el-tag :type="roleTagType(row.role)" effect="light">{{ roleLabel(row.role) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="加入时间" width="120">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.disabled ? 'danger' : 'success'" effect="light">
              {{ row.disabled ? '已停用' : '活跃' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="280">
          <template #default="{ row }">
            <template v-if="row.id !== auth.user?.id">
              <el-button size="small" @click="openRoleDialog(row)">角色变更</el-button>
              <el-button size="small" @click="reset(row)">重置密码</el-button>
              <el-button
                size="small"
                :type="row.disabled ? 'success' : 'danger'"
                plain
                @click="toggleDisabled(row)"
              >
                {{ row.disabled ? '启用' : '停用' }}
              </el-button>
            </template>
            <span v-else class="muted">当前账户</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="keyword ? '没有匹配的用户' : '暂无用户'" />
        </template>
      </el-table>
    </section>

    <el-dialog v-model="roleDialog" title="角色变更" width="440">
      <p v-if="roleTarget" class="role-dialog-text">
        将 <strong>{{ roleTarget.username }}</strong> 的角色从「{{ roleLabel(roleTarget.role) }}」变更为：
      </p>
      <el-select v-model="roleDraft" class="role-select">
        <el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" />
      </el-select>
      <template #footer>
        <el-button @click="roleDialog = false">取消</el-button>
        <el-button
          type="primary"
          :disabled="!roleTarget || roleDraft === roleTarget.role"
          :loading="saving"
          @click="confirmRoleChange"
        >
          确认变更
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="passwordDialog" title="临时密码" width="460">
      <div class="password-row">
        <el-input v-model="temporaryPassword" readonly />
        <el-button type="primary" @click="copyPassword">复制</el-button>
      </div>
      <p class="muted dialog-hint">请立即复制并妥善保存，关闭后将无法再次查看。</p>
    </el-dialog>

    <el-dialog v-model="createDialog" title="新建用户" width="520">
      <el-form label-position="top">
        <el-form-item label="用户名"><el-input v-model="draft.username" /></el-form-item>
        <el-form-item label="显示名"><el-input v-model="draft.display_name" /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="draft.role">
            <el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" />
          </el-select>
        </el-form-item>
        <el-form-item label="初始密码">
          <el-input v-model="draft.password" type="password" show-password placeholder="留空自动生成" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="create">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import { Search } from '@element-plus/icons-vue'
import { createUser, listUsers, resetPassword, updateUser, type UserInfo } from '../api/auth'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const users = ref<UserInfo[]>([])
const keyword = ref('')
const loading = ref(false)
const loadError = ref('')
const saving = ref(false)

// agent 账户供系统内部使用，不在用户管理页展示，也不开放分配。
const roles = ['admin', 'maintainer', 'member', 'viewer']
const roleLabels: Record<string, string> = {
  admin: '管理员',
  maintainer: '维护者',
  member: '成员',
  viewer: '只读'
}

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
    loadError.value = err instanceof Error ? err.message : '用户列表加载失败'
  } finally {
    loading.value = false
  }
}

function roleLabel(role?: string) {
  return (role && roleLabels[role]) || role || '—'
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
    ElMessage.success('角色已更新')
    roleDialog.value = false
    await load()
  } catch (err) {
    showApiError(err, '角色更新失败')
  } finally {
    saving.value = false
  }
}

async function reset(row: UserInfo) {
  try {
    await ElMessageBox.confirm(`确定重置用户「${row.username}」的密码吗？旧密码将立即失效。`, '重置密码', {
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
    showApiError(err, '重置密码失败')
  }
}

async function toggleDisabled(row: UserInfo) {
  const action = row.disabled ? '启用' : '停用'
  const warning = row.disabled ? '' : '停用后该用户的登录态将立即失效。'
  try {
    await ElMessageBox.confirm(`确定${action}用户「${row.username}」吗？${warning}`, `${action}确认`, {
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await updateUser(row.id, { disabled: !row.disabled })
    ElMessage.success(`已${action}`)
    await load()
  } catch (err) {
    showApiError(err, `${action}失败`)
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
    showApiError(err, '创建用户失败')
  } finally {
    saving.value = false
  }
}

async function copyPassword() {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(temporaryPassword.value)
    } else {
      // 内网 HTTP 部署没有 clipboard API，用隐藏 textarea 兜底。
      const el = document.createElement('textarea')
      el.value = temporaryPassword.value
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
    }
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请手动选择复制')
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
