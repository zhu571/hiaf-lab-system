<template>
  <div class="assistant-page">
    <aside class="assistant-side">
      <div class="assistant-side-head">
        <h2>{{ t('assistant.title') }}</h2>
        <div class="assistant-side-actions">
          <el-select v-model="newProject" size="small" class="assistant-project-select">
            <el-option :label="t('assistant.privateSession')" value="" />
            <el-option
              v-for="p in projects"
              :key="p.id"
              :label="p.short_name || p.name"
              :value="p.id"
            />
          </el-select>
          <el-button size="small" type="primary" :loading="creating" @click="createSession">
            {{ t('assistant.new') }}
          </el-button>
        </div>
      </div>
      <el-input
        v-model="search"
        size="small"
        clearable
        class="assistant-search"
        :placeholder="t('assistant.search')"
        @keyup.enter="loadSessions"
        @clear="loadSessions"
      />
      <el-checkbox v-model="showArchived" class="assistant-archived-toggle" @change="loadSessions">
        {{ t('assistant.showArchived') }}
      </el-checkbox>
      <div v-if="usage" class="assistant-usage">
        {{ t('assistant.usage', { used: usage.runs, limit: usage.run_limit }) }}
      </div>
      <div v-if="loadingSessions" v-loading="true" class="assistant-loading" />
      <el-empty v-else-if="!sessions.length" :description="t('assistant.emptySessions')" />
      <ul v-else class="assistant-sessions">
        <li
          v-for="s in sessions"
          :key="s.id"
          :class="['assistant-session', { active: s.id === activeId }]"
          @click="selectSession(s.id)"
        >
          <span class="assistant-session-title">
            {{ s.title || t('assistant.untitled') }}
            <span v-if="s.project_id" class="assistant-shared">{{ t('assistant.shared') }}</span>
          </span>
          <span class="assistant-session-time">{{ s.last_message_at ? formatDate(s.last_message_at) : '' }}</span>
        </li>
      </ul>
    </aside>

    <section class="assistant-main">
      <div v-if="!activeSession" class="assistant-placeholder">
        <el-empty :description="t('assistant.selectSession')" />
      </div>
      <template v-else>
        <header class="assistant-main-head">
          <strong>
            {{ activeSession.title || t('assistant.untitled') }}
            <span v-if="activeSession.project_id" class="assistant-shared">{{ t('assistant.shared') }}</span>
            <span v-if="activeSession.status === 'archived'" class="assistant-shared">{{ t('assistant.archived') }}</span>
          </strong>
          <el-button
            v-if="activeSession.status === 'archived'"
            size="small"
            :loading="restoring"
            @click="restoreActive"
          >
            {{ t('assistant.restore') }}
          </el-button>
          <el-button v-else size="small" :loading="archiving" @click="archiveActive">
            {{ t('assistant.archive') }}
          </el-button>
        </header>
        <div v-if="loadingMessages" v-loading="true" class="assistant-messages-loading" />
        <el-empty v-else-if="!messages.length" :description="t('assistant.emptyMessages')" />
        <div v-else class="assistant-messages">
          <div v-for="m in messages" :key="m.id" :class="['assistant-message', m.role]">
            <div class="assistant-message-role">{{ messageAuthor(m) }}</div>
            <div class="assistant-message-content">{{ messageText(m) }}</div>
          </div>
        </div>
        <div class="assistant-compose">
          <el-input
            v-model="draft"
            type="textarea"
            :rows="3"
            :disabled="activeSession.status === 'archived'"
            :placeholder="t('assistant.placeholder')"
            @keydown.ctrl.enter.prevent="sendMessage"
          />
          <div class="assistant-compose-actions">
            <span class="assistant-hint">{{ t('assistant.runtimePending') }}</span>
            <el-button
              type="primary"
              :loading="sending"
              :disabled="activeSession.status === 'archived' || !draft.trim()"
              @click="sendMessage"
            >
              {{ t('assistant.send') }}
            </el-button>
          </div>
        </div>
      </template>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  archiveAssistantSession,
  createAssistantSession,
  listAssistantMessages,
  listAssistantSessions,
  getAssistantUsage,
  startAssistantRun,
  updateAssistantSession,
  type AssistantMessage,
  type AssistantSession,
  type AssistantUsage
} from '@/api/assistant'
import { listProjects, type Project } from '@/api/projects'
import { showApiError } from '@/composables/useNotify'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const sessions = ref<AssistantSession[]>([])
const messages = ref<AssistantMessage[]>([])
const projects = ref<Project[]>([])
const usage = ref<AssistantUsage | null>(null)
const activeId = ref('')
const newProject = ref('')
const search = ref('')
const showArchived = ref(false)
const draft = ref('')
const loadingSessions = ref(false)
const loadingMessages = ref(false)
const creating = ref(false)
const sending = ref(false)
const archiving = ref(false)
const restoring = ref(false)

const activeSession = computed(() => sessions.value.find((s) => s.id === activeId.value))

function formatDate(value: string) {
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

function roleLabel(role: string) {
  if (role === 'assistant') return t('assistant.role.assistant')
  if (role === 'tool') return t('assistant.role.tool')
  return t('assistant.role.user')
}

function messageAuthor(m: AssistantMessage) {
  if (m.role === 'user' && m.user_id && m.user_id !== auth.user?.id) return t('assistant.role.member')
  return roleLabel(m.role)
}

function messageText(m: AssistantMessage) {
  return m.content_json?.placeholder ? t('assistant.runtimeReply') : m.content
}

async function loadSessions() {
  loadingSessions.value = true
  try {
    const res = await listAssistantSessions({
      status: showArchived.value ? 'archived' : 'active',
      q: search.value.trim()
    })
    sessions.value = res.items
    if (!activeId.value && sessions.value.length) activeId.value = sessions.value[0].id
    if (activeId.value) await loadMessages()
  } catch (err) {
    showApiError(err, t('assistant.loadFailed'))
  } finally {
    loadingSessions.value = false
  }
}

async function loadMessages() {
  if (!activeId.value) return
  loadingMessages.value = true
  try {
    const res = await listAssistantMessages(activeId.value)
    messages.value = res.items
  } catch (err) {
    showApiError(err, t('assistant.messagesFailed'))
  } finally {
    loadingMessages.value = false
  }
}

async function loadProjects() {
  try {
    projects.value = await listProjects('active')
  } catch {
    projects.value = []
  }
}

async function loadUsage() {
  try {
    usage.value = await getAssistantUsage()
  } catch {
    usage.value = null
  }
}

function selectSession(id: string) {
  activeId.value = id
  messages.value = []
  void loadMessages()
}

async function createSession() {
  creating.value = true
  try {
    const session = await createAssistantSession('', newProject.value)
    sessions.value = [session, ...sessions.value]
    activeId.value = session.id
    messages.value = []
  } catch (err) {
    showApiError(err, t('assistant.createFailed'))
  } finally {
    creating.value = false
  }
}

async function sendMessage() {
  const content = draft.value.trim()
  if (!content || !activeId.value) return
  sending.value = true
  try {
    const result = await startAssistantRun(activeId.value, content)
    messages.value.push(...result.messages)
    draft.value = ''
    void loadUsage()
  } catch (err) {
    showApiError(err, t('assistant.sendFailed'))
  } finally {
    sending.value = false
  }
}

async function archiveActive() {
  if (!activeId.value) return
  archiving.value = true
  try {
    await archiveAssistantSession(activeId.value)
    sessions.value = sessions.value.filter((s) => s.id !== activeId.value)
    activeId.value = sessions.value[0]?.id ?? ''
    messages.value = []
    if (activeId.value) await loadMessages()
  } catch (err) {
    showApiError(err, t('assistant.archiveFailed'))
  } finally {
    archiving.value = false
  }
}

async function restoreActive() {
  if (!activeId.value) return
  restoring.value = true
  try {
    await updateAssistantSession(activeId.value, { status: 'active' })
    sessions.value = sessions.value.filter((s) => s.id !== activeId.value)
    activeId.value = sessions.value[0]?.id ?? ''
    messages.value = []
    if (activeId.value) await loadMessages()
  } catch (err) {
    showApiError(err, t('assistant.restoreFailed'))
  } finally {
    restoring.value = false
  }
}

onMounted(() => {
  void loadProjects()
  void loadSessions()
  void loadUsage()
})
</script>

<style scoped>
.assistant-page {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: var(--space-4);
  height: calc(100vh - 140px);
}
.assistant-side,
.assistant-main {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  overflow: auto;
}
.assistant-side-head,
.assistant-main-head,
.assistant-compose-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}
.assistant-side-head h2 {
  margin: 0;
  font-size: var(--fs-body);
}
.assistant-side-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.assistant-project-select {
  width: 110px;
}
.assistant-search {
  margin-top: var(--space-2);
}
.assistant-archived-toggle {
  margin-top: var(--space-1);
}
.assistant-usage {
  margin-top: var(--space-1);
  font-size: var(--fs-body-sm);
  color: var(--text-3);
}
.assistant-shared {
  margin-left: var(--space-1);
  padding: 0 var(--space-1);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: var(--fs-body-sm);
  color: var(--text-3);
}
.assistant-loading,
.assistant-messages-loading {
  height: 120px;
}
.assistant-sessions {
  list-style: none;
  margin: var(--space-2) 0 0;
  padding: 0;
}
.assistant-session {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: var(--space-2);
  border-radius: var(--radius-sm);
  cursor: pointer;
  color: var(--text-2);
}
.assistant-session.active {
  background: var(--surface-2);
  color: var(--text-1);
}
.assistant-session-time {
  font-size: var(--fs-body-sm);
  color: var(--text-3);
}
.assistant-main {
  display: flex;
  flex-direction: column;
}
.assistant-messages {
  flex: 1;
  overflow: auto;
  margin: var(--space-3) 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.assistant-message {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  background: var(--surface-2);
}
.assistant-message.user {
  align-self: flex-end;
  max-width: 80%;
}
.assistant-message.assistant {
  align-self: flex-start;
  max-width: 80%;
}
.assistant-message-role {
  font-size: var(--fs-body-sm);
  color: var(--text-3);
  margin-bottom: 2px;
}
.assistant-message-content {
  white-space: pre-wrap;
  word-break: break-word;
}
.assistant-compose {
  margin-top: auto;
}
.assistant-compose-actions {
  margin-top: var(--space-2);
}
.assistant-hint {
  font-size: var(--fs-body-sm);
  color: var(--text-3);
}
.assistant-placeholder {
  display: flex;
  height: 100%;
  align-items: center;
  justify-content: center;
}
</style>
