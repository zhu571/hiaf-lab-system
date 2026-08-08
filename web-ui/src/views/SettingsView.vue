<template>
  <div class="page settings">
    <section v-if="isMobile" class="panel mobile-quick-links">
      <h3 class="section-title">{{ t('settings.quickLinks') }}</h3>
      <div class="quick-card-row">
        <div class="quick-card" role="button" tabindex="0" @click="router.push('/experiences')" @keydown.enter="router.push('/experiences')">
          <el-icon><Memo /></el-icon>
          <span>{{ t('nav.experiences') }}</span>
        </div>
        <div class="quick-card" role="button" tabindex="0" @click="router.push('/attachments')" @keydown.enter="router.push('/attachments')">
          <el-icon><Paperclip /></el-icon>
          <span>{{ t('nav.attachments') }}</span>
        </div>
      </div>
    </section>
    <section class="panel settings-card">
      <div class="user-head">
        <span class="avatar">{{ (auth.user?.username || '?').slice(0, 1).toUpperCase() }}</span>
        <div class="user-meta">
          <h2>{{ t('settings.title') }}</h2>
          <p class="muted">{{ auth.user?.username }} · {{ auth.user?.role }}</p>
        </div>
      </div>
      <el-alert v-if="auth.user?.must_change_password" :title="t('settings.mustChangePassword')" type="warning" show-icon :closable="false" />
      <div class="language-row">
        <span class="language-label">{{ t('settings.language') }}</span>
        <el-select :model-value="locale" style="width: 160px" @change="onLanguageChange">
          <el-option label="中文" value="zh" />
          <el-option label="English" value="en" />
        </el-select>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('settings.oldPassword')"><el-input v-model="form.oldPassword" type="password" show-password /></el-form-item>
        <el-form-item :label="t('settings.newPassword')"><el-input v-model="form.newPassword" type="password" show-password /></el-form-item>
        <el-form-item :label="t('settings.confirmNewPassword')"><el-input v-model="form.confirm" type="password" show-password /></el-form-item>
        <div class="form-actions">
          <el-button type="primary" native-type="submit">{{ t('settings.changePassword') }}</el-button>
          <el-button @click="doLogout">{{ t('nav.logout') }}</el-button>
        </div>
      </el-form>
    </section>

    <!-- 系统更新卡片 — 仅 admin 可见 -->
    <section v-if="auth.isAdmin" class="panel update-card">
      <h3 class="section-title">{{ t('settings.systemUpdate') }}</h3>

      <!-- 版本信息 -->
      <div class="version-row">
        <div class="version-item">
          <span class="version-label">{{ t('settings.currentVersion') }}</span>
          <el-tag v-if="version?.current_short" type="info">{{ version.current_short }}</el-tag>
          <span v-else class="muted">—</span>
        </div>
        <el-icon class="version-arrow"><ArrowRight /></el-icon>
        <div class="version-item">
          <span class="version-label">{{ t('settings.latestVersion') }}</span>
          <el-tag v-if="version?.latest_short" :type="version.behind > 0 ? 'warning' : 'success'">
            {{ version.latest_short }}
          </el-tag>
          <span v-else class="muted">{{ t('settings.versionCheckFailed') }}</span>
        </div>
        <span v-if="version && version.behind > 0" class="behind-badge">
          {{ t('settings.commitsBehind', { n: version.behind }) }}
        </span>
      </div>

      <!-- 操作按钮 -->
      <div class="update-actions">
        <el-button :loading="versionLoading" @click="refreshVersion">
          {{ t('settings.checkUpdate') }}
        </el-button>
        <el-button
          type="primary"
          :disabled="!version?.can_update || updateRunning"
          :loading="updateStarting"
          @click="startUpdate"
        >
          {{ t('settings.startUpdate') }}
        </el-button>
        <span v-if="version && !version.can_update && !versionLoading" class="hint muted">
          {{ t('settings.cannotUpdate') }}
        </span>
      </div>

      <!-- 更新日志区域 -->
      <div v-if="updateSessionId" class="update-log">
        <div class="log-header">
          <span v-if="updateRunning" class="running-indicator">
            <span class="pulse" /> {{ t('settings.updating') }}
          </span>
          <span v-else-if="updateResult === 'success'" class="result-ok">
            <el-icon><CircleCheckFilled /></el-icon> {{ t('settings.updateSuccess') }}
          </span>
          <span v-else-if="updateResult === 'failed'" class="result-fail">
            <el-icon><CircleCloseFilled /></el-icon> {{ t('settings.updateFailed') }}
          </span>
          <span class="log-stats">{{ t('settings.logLines', { n: logLines.length }) }}</span>
        </div>
        <div ref="logContainer" class="log-terminal">
          <pre><code><template v-for="(line, i) in logLines" :key="i"><span :class="logLineClass(line)" v-text="line" />
</template></code></pre>
        </div>
      </div>

      <!-- 重连提示 -->
      <el-alert
        v-if="updateRunning && streamDisconnected"
        :title="t('settings.reconnecting')"
        type="warning"
        :closable="false"
        show-icon
      />
    </section>

    <section v-if="quickLinks.length" class="panel quick-links">
      <h3 class="section-title">{{ t('settings.quickLinks') }}</h3>
      <el-link v-for="link in quickLinks" :key="link.path" :underline="false" @click="router.push(link.path)">
        <el-icon style="margin-right:6px"><component :is="link.icon" /></el-icon>
        {{ link.label }}
      </el-link>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowRight, CircleCheckFilled, CircleCloseFilled, DataBoard, Memo, Paperclip, Tickets, User } from '@element-plus/icons-vue'
import { changePassword } from '../api/auth'
import { refreshAuthSession } from '../api/client'
import * as systemApi from '../api/system'
import type { SSEEvent, VersionInfo } from '../api/system'
import { useAuthStore } from '../stores/auth'
import { useMobile } from '../composables/useMobile'
import { setLocale, type AppLocale } from '../i18n'

const router = useRouter()
const auth = useAuthStore()
const { t, locale } = useI18n()
const isMobile = useMobile()
const form = reactive({ oldPassword: '', newPassword: '', confirm: '' })

// ---- 版本状态 ----
const version = ref<VersionInfo | null>(null)
const versionLoading = ref(false)

// ---- 更新状态 ----
const updateSessionId = ref<string | null>(null)
const updateRunning = ref(false)
const updateStarting = ref(false)
const updateResult = ref<'success' | 'failed' | null>(null)
const logLines = ref<string[]>([])
const logContainer = ref<HTMLElement>()
const streamDisconnected = ref(false)
let streamClose: (() => void) | undefined
let reconnectTimer: ReturnType<typeof setTimeout> | undefined
let lastSeq = 0 // 已消费的最大 seq，用于重连去重

// ---- session 持久化：页面刷新后可恢复同一更新会话的日志流 ----
const SESSION_KEY = 'lab.system.update.session_id'

function persistSession(id: string | null) {
  if (id) localStorage.setItem(SESSION_KEY, id)
  else localStorage.removeItem(SESSION_KEY)
}

// ---- 重连退避参数（指数退避 + 抖动） ----
const RECONNECT_BASE_MS = 500 // 初始 500ms
const RECONNECT_MAX_MS = 15000 // 上限 15s
const RECONNECT_ATTEMPTS = 10 // 最多 10 次
let reconnectAttempts = 0
let authFailed = false // 仅 401 时置位，重连前才刷新 token

interface QuickLink { label: string; path: string; icon: any }

const quickLinks = computed<QuickLink[]>(() => {
  const links: QuickLink[] = []
  if (auth.canReviewAgent) links.push({ label: t('nav.aiReview'), path: '/agent-candidates', icon: Tickets })
  if (auth.isAdmin) links.push({ label: t('nav.adminUsers'), path: '/admin/users', icon: User })
  links.push({ label: t('nav.audit'), path: '/audit', icon: DataBoard })
  return links
})

onMounted(async () => {
  // 非 admin 不发起任何系统更新相关请求（换账号后残留的 session_id 也必然 403）
  if (!auth.isAdmin) return
  await refreshVersion()
  // 页面刷新后恢复上次未结束的更新会话（localStorage 持久化 session_id）
  const saved = localStorage.getItem(SESSION_KEY)
  if (saved) {
    updateSessionId.value = saved
    updateRunning.value = true
    updateResult.value = null
    logLines.value = []
    lastSeq = 0
    reconnectAttempts = 0
    streamDisconnected.value = false
    connectStream(saved)
  }
})

onBeforeUnmount(() => {
  streamClose?.()
  clearTimeout(reconnectTimer)
})

// ---- 版本刷新 ----
async function refreshVersion() {
  versionLoading.value = true
  try {
    version.value = await systemApi.getVersion()
  } catch (err) {
    version.value = null
    // 网络/后端错误也要给出提示（含 request_id，便于对齐审计日志）
    const e = err as Error & { requestId?: string }
    ElMessage.error(e.requestId ? `${e.message} (request_id: ${e.requestId})` : e.message)
  } finally {
    versionLoading.value = false
  }
}

// ---- 触发更新 ----
async function startUpdate() {
  updateStarting.value = true
  try {
    const { data } = await systemApi.triggerUpdate()
    updateSessionId.value = data.session_id
    persistSession(data.session_id)
    updateRunning.value = true
    updateResult.value = null
    logLines.value = []
    lastSeq = 0
    reconnectAttempts = 0
    streamDisconnected.value = false
    connectStream(data.session_id)
  } catch (err) {
    const e = err as Error & { requestId?: string }
    ElMessage.error(e.requestId ? `${e.message} (request_id: ${e.requestId})` : e.message)
  } finally {
    updateStarting.value = false
  }
}

// ---- SSE 连接 ----
function connectStream(sessionId: string) {
  streamClose?.()
  const { close } = systemApi.connectUpdateStream(sessionId, {
    onEvent: (event: SSEEvent) => {
      streamDisconnected.value = false
      if (event.seq <= lastSeq) return // 历史回放 + 实时帧去重
      lastSeq = event.seq
      reconnectAttempts = 0 // 仅收到真正的新帧才复位退避计数（回放帧不复位）
      if (event.type === 'line' || event.type === 'step') {
        const text = event.type === 'step'
          ? `\n===== ${t('settings.stepLabel', { step: event.step, total: event.step_total })}：${event.title} =====\n`
          : event.text
        logLines.value.push(text)
        if (logLines.value.length > 2000) logLines.value.splice(0, logLines.value.length - 2000)
        nextTick(() => scrollLogToBottom())
      } else if (event.type === 'error') {
        logLines.value.push(`[ERROR] ${event.message}`)
        nextTick(() => scrollLogToBottom())
      } else if (event.type === 'done') {
        updateRunning.value = false
        updateResult.value = event.success ? 'success' : 'failed'
        void refreshVersion() // 更新完成后自动刷新版本信息
        clearTimeout(reconnectTimer)
        persistSession(null) // done 后终止：清除持久化 session，避免刷新后误恢复
        streamClose?.() // done 后主动关闭 SSE 连接（服务端随即断开）
      }
    },
    onAuthError: () => {
      // 401：token 过期。置位标记，重连前先走 client 单飞 refresh 刷新 Cookie
      if (updateRunning.value) {
        streamDisconnected.value = true
        authFailed = true
        scheduleReconnect(sessionId)
      }
    },
    onHttpError: (status: number) => {
      // 非 401 的 HTTP 层错误，不当作网络错误盲重连：
      // 404 = session 已不存在（TTL 过期 sweep/重启丢失），重连无意义，直接终止；
      //       但 404 不代表更新失败，只提示无法恢复日志；
      // 其他 4xx（如 403 无权限）= 重试无意义，同样终止；
      // 409 = 订阅者过多，非网络问题，退避后重试（仍受总次数上限约束）。
      if (!updateRunning.value) return
      if (status === 404) {
        updateRunning.value = false
        streamDisconnected.value = false
        clearTimeout(reconnectTimer)
        persistSession(null)
        ElMessage.warning(t('settings.sessionNotFound'))
      } else if (status >= 400 && status < 500 && status !== 409) {
        updateRunning.value = false
        streamDisconnected.value = false
        clearTimeout(reconnectTimer)
        persistSession(null)
        ElMessage.error(t('settings.streamHttpError', { status }))
      } else {
        streamDisconnected.value = true
        scheduleReconnect(sessionId)
      }
    },
    onNetworkError: () => {
      // 网络/服务中断（非 HTTP 层错误）：不刷新 token，直接按退避重连
      if (updateRunning.value) {
        streamDisconnected.value = true
        scheduleReconnect(sessionId)
      }
    }
  })
  streamClose = close
}

function scheduleReconnect(sessionId: string) {
  if (reconnectAttempts >= RECONNECT_ATTEMPTS) {
    updateRunning.value = false
    // 保留 session_id：服务端可能仍在执行，刷新页面可再次尝试恢复；仅停止自动重连并提示
    ElMessage.warning(t('settings.reconnectExhausted'))
    return
  }
  clearTimeout(reconnectTimer)
  reconnectAttempts++
  const backoff = Math.min(RECONNECT_BASE_MS * 2 ** reconnectAttempts, RECONNECT_MAX_MS)
  const jitter = Math.floor(Math.random() * 250)
  reconnectTimer = setTimeout(() => reconnectStream(sessionId), backoff + jitter)
}

async function reconnectStream(sessionId: string) {
  // 仅 401 才刷新 token（authFailed 由 onAuthError 置位）；网络中断直接重连。
  // 复用 client.ts 的单飞 refresh（与 axios 401 重试共享，避免双 refresh 竞态），
  // 成功时同步更新 CSRF token。
  if (authFailed) {
    authFailed = false
    try {
      if (!(await refreshAuthSession())) throw new Error('refresh failed')
    } catch {
      updateRunning.value = false
      updateResult.value = 'failed'
      streamDisconnected.value = true
      persistSession(null) // 防页面刷新后 onMounted 恢复 → 401 → 刷新失败 → 再次跳登录的循环
      // token 已彻底失效：整页跳登录（清空内存态，路由守卫重新鉴权）
      ElMessage.error(t('settings.sessionExpired'))
      window.location.assign('/login')
      return
    }
  }
  connectStream(sessionId)
}

function scrollLogToBottom() {
  const el = logContainer.value
  if (el) el.scrollTop = el.scrollHeight
}

function logLineClass(line: string): string {
  if (line.startsWith('[ERROR]')) return 'log-error'
  if (line.startsWith('[WARN]')) return 'log-warn'
  return ''
}

async function onLanguageChange(value: string | number | boolean) {
  const lang = value as AppLocale
  const prev = locale.value as AppLocale
  setLocale(lang) // 本地先生效，后端保存失败再回滚
  try {
    await auth.setLanguage(lang)
    ElMessage.success(t('settings.languageSaved'))
  } catch {
    setLocale(prev)
    ElMessage.error(t('settings.languageSaveFailed'))
  }
}

async function submit() {
  if (form.newPassword !== form.confirm) {
    ElMessage.error(t('settings.passwordMismatch'))
    return
  }
  try {
    await changePassword(form.oldPassword, form.newPassword)
    await auth.loadMe()
    ElMessage.success(t('settings.passwordChanged'))
  } catch (err) {
    const e = err as Error & { requestId?: string }
    // 展示后端错误信息 + request_id，便于对齐审计日志
    const detail = e.message || t('settings.passwordChangeFailed')
    ElMessage.error(e.requestId ? `${detail} (request_id: ${e.requestId})` : detail)
  }
}

async function doLogout() {
  try {
    await auth.logout()
  } catch {
    // 后端登出失败也强制回登录页，前端态由路由守卫重置
  }
  await router.push('/login')
}
</script>

<style scoped>
.settings {
  margin: 0 auto;
  max-width: 640px;
  width: 100%;
}

.settings-card {
  display: grid;
  gap: 20px;
}

.user-head {
  align-items: center;
  display: flex;
  gap: 14px;
}

.avatar {
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 50%;
  box-shadow: 0 6px 16px -6px rgba(20, 112, 138, 0.5);
  color: #fff;
  display: grid;
  flex-shrink: 0;
  font-size: 18px;
  font-weight: 700;
  height: 46px;
  place-items: center;
  width: 46px;
}

.user-meta {
  display: grid;
  gap: 2px;
}

.language-row {
  align-items: center;
  display: flex;
  gap: 12px;
}

.language-label {
  color: var(--text-2);
  font-size: 14px;
}

.form-actions {
  display: flex;
  gap: 12px;
}

.quick-links {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 20px;
}

.mobile-quick-links {
  display: grid;
  gap: 12px;
}

.quick-card-row {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.quick-card {
  align-items: center;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--text-2);
  cursor: pointer;
  display: flex;
  flex-direction: column;
  font-size: 14px;
  gap: 8px;
  min-height: 76px;
  justify-content: center;
  transition: border-color 0.15s ease;
}

.quick-card:active {
  background: var(--brand-050);
  border-color: var(--brand-500);
}

.quick-card .el-icon {
  color: var(--brand-600);
  font-size: 22px;
}

.section-title {
  color: var(--text-secondary, #6b7280);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.5px;
  margin: 0;
  text-transform: uppercase;
  width: 100%;
}

.quick-links .el-link {
  align-items: center;
  background: var(--bg-panel, #fff);
  border: 1px solid var(--border-light, #e5e7eb);
  border-radius: 8px;
  display: flex;
  padding: 10px 16px;
}

/* ---- 系统更新卡片 ---- */
.update-card {
  display: grid;
  gap: 12px;
  margin-top: 20px;
}

.version-row {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.version-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.version-label {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.version-arrow {
  color: var(--el-text-color-secondary);
}

.behind-badge {
  background: var(--el-color-warning-light-9);
  border-radius: 4px;
  color: var(--el-color-warning);
  font-size: 12px;
  padding: 2px 8px;
}

.update-actions {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.update-log {
  margin-top: 16px;
}

.log-header {
  align-items: center;
  display: flex;
  justify-content: space-between;
  margin-bottom: 6px;
}

.log-stats {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.log-terminal {
  background: #1e1e1e;
  border-radius: 6px;
  color: #d4d4d4;
  font-family: 'Cascadia Code', 'Fira Code', 'JetBrains Mono', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.6;
  max-height: 480px;
  overflow-y: auto;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}

.log-terminal .log-error {
  color: #f44747;
}

.log-terminal .log-warn {
  color: #e5c07b;
}

.running-indicator {
  align-items: center;
  color: var(--el-color-primary);
  display: flex;
  font-size: 13px;
  gap: 6px;
}

.pulse {
  animation: pulse 1.5s infinite;
  background: var(--el-color-primary);
  border-radius: 50%;
  height: 8px;
  width: 8px;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.3;
  }
}

.result-ok {
  align-items: center;
  color: var(--el-color-success);
  display: flex;
  font-size: 13px;
  gap: 4px;
}

.result-fail {
  align-items: center;
  color: var(--el-color-danger);
  display: flex;
  font-size: 13px;
  gap: 4px;
}
</style>
