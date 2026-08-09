<template>
  <el-drawer v-model="askOpen" :size="isMobile ? '96%' : '720px'" :title="t('ask.title')" class="ask-drawer">
    <div class="ask-shell">
      <el-tabs v-model="activeTab" class="ask-tabs">
        <el-tab-pane :label="t('ask.chat')" name="chat">
          <div ref="chatArea" class="chat-area">
            <div v-if="turns.length === 0" class="ask-empty">
              <el-empty :description="t('ask.empty')" />
            </div>
            <div v-for="turn in turns" :key="turn.id" class="turn">
              <div class="chat-message user">
                <div class="chat-bubble user-bubble">{{ turn.question }}</div>
              </div>
              <div class="chat-message assistant">
                <div class="chat-bubble">
                  <p v-if="turn.failed" class="ask-error">{{ turn.error }}</p>
                  <AskResultPanel v-else-if="turn.result" :data="turn.result" @open="openRoute" />
                </div>
              </div>
            </div>
            <div v-if="sending" class="chat-message assistant">
              <div class="chat-bubble chat-loading">{{ t('ask.thinking') }}</div>
            </div>
          </div>
          <div class="chat-input">
            <el-input
              v-model="question"
              type="textarea"
              :rows="2"
              resize="none"
              :disabled="sending"
              :placeholder="t('ask.placeholder')"
              @keydown="onKeydown"
            />
            <el-button
              class="send-btn"
              type="primary"
              :loading="sending"
              :disabled="!question.trim() || sending"
              @click="send"
            >
              {{ t('ask.send') }}
            </el-button>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="t('ask.history')" name="history">
          <div v-if="detail" class="history-detail">
            <div class="history-detail-head">
              <el-button link type="primary" @click="detail = null">
                <el-icon class="back-icon"><ArrowLeft /></el-icon>{{ t('ask.backToHistory') }}
              </el-button>
              <span class="history-q">{{ detail.question }}</span>
            </div>
            <AskResultPanel v-if="detailData" :data="detailData" @open="openRoute" />
          </div>
          <template v-else>
            <div v-loading="historyLoading" class="history-list">
              <div v-if="!historyLoading && history.length === 0" class="ask-empty">
                <el-empty :description="t('ask.emptyHistory')" />
              </div>
              <button
                v-for="item in history"
                :key="item.id"
                type="button"
                class="history-item"
                @click="openHistory(item)"
              >
                <span class="history-main">
                  <strong class="history-q">{{ item.question }}</strong>
                  <span class="history-meta">
                    {{ formatTime(item.created_at) }} · {{ item.table_name || '-' }} ·
                    {{ t('ask.rowCount', { n: item.row_count }) }}
                  </span>
                </span>
                <el-icon class="history-chevron"><ArrowRight /></el-icon>
              </button>
            </div>
            <el-pagination
              v-model:current-page="historyPage"
              class="pager"
              layout="total, prev, pager, next"
              :total="historyTotal"
              :page-size="historyPerPage"
              @current-change="loadHistory"
            />
          </template>
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight } from '@element-plus/icons-vue'
import AskResultPanel, { type AskResultData } from './AskResultPanel.vue'
import { useAskDialog } from '../composables/useAskDialog'
import { useMobile } from '../composables/useMobile'
import { askChat, askHistory, askHistoryDetail, type AskHistoryDetail as AskHistoryDetailType, type AskHistoryItem } from '../api/ask'
import { newIdempotencyKey } from '../api/client'
import { showApiError } from '../composables/useNotify'

type Turn = {
  id: string
  question: string
  failed: boolean
  error: string
  result: AskResultData | null
}

const { t } = useI18n()
const router = useRouter()
const isMobile = useMobile()
const { askOpen } = useAskDialog()

const activeTab = ref('chat')
const question = ref('')
const sending = ref(false)
const turns = ref<Turn[]>([])
const chatArea = ref<HTMLElement | null>(null)

// 历史 tab
const history = ref<AskHistoryItem[]>([])
const historyTotal = ref(0)
const historyPage = ref(1)
const historyPerPage = 20
const historyLoading = ref(false)
const detail = ref<AskHistoryDetailType | null>(null)

const detailData = computed<AskResultData | null>(() => {
  const d = detail.value
  if (!d) return null
  return {
    answer: d.answer,
    sql: d.sql_text,
    tableName: d.table_name,
    columns: d.columns ?? [],
    rows: d.rows ?? [],
    rowCount: d.row_count ?? 0,
    truncated: false,
    durationMs: d.duration_ms
  }
})

let turnSeq = 0

function makeTurn(questionText: string): Turn {
  turnSeq += 1
  return { id: `turn_${turnSeq}_${Date.now()}`, question: questionText, failed: false, error: '', result: null }
}

function scrollToBottom() {
  nextTick(() => {
    const el = chatArea.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

// Enter 发送，Ctrl+Enter 换行（textarea 默认行为，不做 preventDefault）
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey) {
    e.preventDefault()
    send()
  }
}

async function send() {
  const q = question.value.trim()
  if (!q || sending.value) return
  const turn = makeTurn(q)
  turns.value.push(turn)
  question.value = ''
  sending.value = true
  scrollToBottom()
  try {
    const res = await askChat(q, newIdempotencyKey())
    turn.result = {
      answer: res.answer,
      sql: res.sql,
      tableName: res.table_name,
      columns: res.columns ?? [],
      rows: res.rows ?? [],
      rowCount: res.row_count ?? 0,
      truncated: res.truncated,
      durationMs: res.duration_ms
    }
  } catch (err) {
    turn.failed = true
    turn.error = (err as Error).message || t('ask.error')
    showApiError(err, t('ask.error'))
  } finally {
    sending.value = false
    scrollToBottom()
  }
}

// 打开历史 tab 时加载列表（抽屉常驻，切 tab 即刷新）
watch(activeTab, (tab) => {
  if (tab === 'history') loadHistory()
})

async function loadHistory() {
  // 清 detail 状态：切 tab / 翻页后回到列表视图，避免残留上一条明细
  detail.value = null
  historyLoading.value = true
  try {
    const data = await askHistory({ page: historyPage.value, per_page: historyPerPage })
    history.value = data.items ?? []
    historyTotal.value = data.total ?? 0
  } catch (err) {
    showApiError(err, t('ask.historyLoadFailed'))
  } finally {
    historyLoading.value = false
  }
}

// 点击历史条目 → 明细接口拿 rows 快照还原表格（不重新查询）
async function openHistory(item: AskHistoryItem) {
  try {
    detail.value = await askHistoryDetail(item.id)
  } catch (err) {
    showApiError(err, t('ask.detailLoadFailed'))
  }
}

function formatTime(x: string) {
  if (!x) return ''
  return new Date(x).toLocaleString('zh-CN', { hour12: false })
}

function openRoute(route: string) {
  askOpen.value = false
  router.push(route)
}
</script>

<style scoped>
.ask-drawer :deep(.el-drawer__body) {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0 20px 20px;
}

.ask-shell {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
}

.ask-tabs {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  padding-top: 8px;
}

.ask-tabs :deep(.el-tabs__header) {
  flex-shrink: 0;
}

.ask-tabs :deep(.el-tabs__content) {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.ask-tabs :deep(.el-tab-pane) {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.chat-area {
  display: grid;
  flex: 1;
  gap: 14px;
  min-height: 0;
  overflow-y: auto;
  padding: 4px 2px 12px;
}

.ask-empty {
  padding: 36px 0;
}

.turn {
  display: grid;
  gap: 10px;
}

.chat-message {
  display: flex;
}

.chat-message.user {
  justify-content: flex-end;
}

.chat-bubble {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 12px 12px 12px 2px;
  max-width: 94%;
  padding: 10px 12px;
}

.chat-message.user .chat-bubble {
  background: var(--brand-100);
  border: none;
  border-radius: 12px 12px 2px 12px;
  color: var(--text-1);
  font-weight: 500;
  max-width: 80%;
  white-space: pre-wrap;
  word-break: break-word;
}

.chat-loading {
  color: var(--text-3);
  font-size: 13px;
}

.ask-error {
  color: var(--danger);
  white-space: pre-wrap;
  word-break: break-word;
}

.chat-input {
  align-items: flex-end;
  border-top: 1px solid var(--border);
  display: flex;
  gap: 10px;
  padding-top: 12px;
}

.chat-input .el-textarea {
  flex: 1;
}

.send-btn {
  flex-shrink: 0;
}

.history-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.history-item {
  align-items: center;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  cursor: pointer;
  display: flex;
  gap: 12px;
  margin-bottom: 10px;
  padding: 12px 14px;
  text-align: left;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease;
  width: 100%;
}

.history-item:hover {
  border-color: var(--brand-500);
  box-shadow: var(--shadow-sm);
}

.history-main {
  display: grid;
  flex: 1;
  gap: 4px;
  min-width: 0;
}

.history-q {
  color: var(--text-1);
  font-size: 14px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-meta {
  color: var(--text-3);
  font-size: 12px;
}

.history-chevron {
  color: var(--text-3);
  flex-shrink: 0;
}

.history-detail {
  display: grid;
  gap: 14px;
}

.history-detail-head {
  align-items: center;
  display: flex;
  gap: 10px;
}

.back-icon {
  margin-right: 2px;
}

.pager {
  flex-shrink: 0;
  justify-content: flex-end;
  margin-top: 8px;
}

@media (max-width: 768px) {
  .chat-bubble {
    max-width: 100%;
  }
}
</style>
