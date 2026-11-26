<template>
  <div class="assistant-page">
    <aside class="assistant-side">
      <div class="assistant-side-head">
        <h2>{{ t('assistant.title') }}</h2>
        <el-button size="small" type="primary" :loading="creating" @click="createSession">
          {{ t('assistant.new') }}
        </el-button>
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
          <span class="assistant-session-title">{{ s.title || t('assistant.untitled') }}</span>
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
          <strong>{{ activeSession.title || t('assistant.untitled') }}</strong>
          <el-button size="small" :loading="archiving" @click="archiveActive">
            {{ t('assistant.archive') }}
          </el-button>
        </header>
        <div v-if="loadingMessages" v-loading="true" class="assistant-messages-loading" />
        <el-empty v-else-if="!messages.length" :description="t('assistant.emptyMessages')" />
        <div v-else class="assistant-messages">
          <div v-for="m in messages" :key="m.id" :class="['assistant-message', m.role]">
            <div class="assistant-message-role">{{ roleLabel(m.role) }}</div>
            <div class="assistant-message-content">{{ messageText(m) }}</div>
          </div>
        </div>
        <div class="assistant-compose">
          <el-input
            v-model="draft"
            type="textarea"
            :rows="3"
            :placeholder="t('assistant.placeholder')"
            @keydown.ctrl.enter.prevent="sendMessage"
          />
          <div class="assistant-compose-actions">
            <span class="assistant-hint">{{ t('assistant.runtimePending') }}</span>
            <el-button type="primary" :loading="sending" :disabled="!draft.trim()" @click="sendMessage">
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
  startAssistantRun,
  type AssistantMessage,
  type AssistantSession
} from '@/api/assistant'

const { t } = useI18n()
const sessions = ref<AssistantSession[]>([])
const messages = ref<AssistantMessage[]>([])
const activeId = ref('')
const draft = ref('')
const loadingSessions = ref(false)
const loadingMessages = ref(false)
const creating = ref(false)
const sending = ref(false)
const archiving = ref(false)

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

function messageText(m: AssistantMessage) {
  return m.content_json?.placeholder ? t('assistant.runtimeReply') : m.content
}

async function loadSessions() {
  loadingSessions.value = true
  try {
    const res = await listAssistantSessions({ status: 'active' })
    sessions.value = res.items
    if (!activeId.value && sessions.value.length) activeId.value = sessions.value[0].id
    if (activeId.value) await loadMessages()
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
  } finally {
    loadingMessages.value = false
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
    const session = await createAssistantSession()
    sessions.value = [session, ...sessions.value]
    activeId.value = session.id
    messages.value = []
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
  } finally {
    archiving.value = false
  }
}

onMounted(loadSessions)
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
