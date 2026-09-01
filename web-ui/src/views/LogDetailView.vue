<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('logDetail.title') }}</h2>
      <el-button @click="goBack">{{ t('logDetail.back') }}</el-button>
    </div>
    <StateBlock
      :loading="loading"
      :error="error && !notFound ? error : null"
      :error-text="t('logDetail.loadFailed')"
      @retry="run"
    >
      <el-empty v-if="notFound" :description="t('logDetail.notFound')" />
      <template v-else-if="log">
        <section class="panel">
          <el-descriptions border :column="isMobile ? 1 : 2" size="small">
            <el-descriptions-item :label="t('logDetail.project')">
              <el-button link type="primary" @click="goProject">{{ projectName }}</el-button>
            </el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.occurredAt')">{{ formatDateTime(log.occurred_at) }}</el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.category')">{{ categoryLabel(log.category) }}</el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.author')">{{ log.author_id }}</el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.source')">{{ sourceLabel(log.source) }}</el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.status')"><StatusBadge domain="logStatus" :value="log.content_status" /></el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.translationStatus')"><StatusBadge domain="translation" :value="contentLocalized.status" /></el-descriptions-item>
            <el-descriptions-item :label="t('logDetail.createdAt')">{{ formatDateTime(log.created_at) }}</el-descriptions-item>
          </el-descriptions>
        </section>
        <section class="panel">
          <h3>{{ t('logDetail.content') }}</h3>
          <p class="log-content">{{ contentLocalized.text }}</p>
        </section>
        <section v-if="log.raw_snippet" class="panel">
          <el-collapse>
            <el-collapse-item :title="t('logDetail.rawSnippet')">
              <p class="raw-snippet">{{ log.raw_snippet }}</p>
            </el-collapse-item>
          </el-collapse>
        </section>
        <section class="panel">
          <h3>{{ t('logDetail.attachments') }}</h3>
          <AttachmentList entity-type="log" :entity-id="log.id" />
        </section>
      </template>
    </StateBlock>
  </div>
</template>

<script setup lang="ts">
// 日志详情页（log-view-optimization 批 W5）：GET /logs/{id} 只返回日志本体 + translations
// sidecar（附件走 AttachmentList 单独查 entity_type=log；契约宣称的关联 issue/仪器实现没有，
// 诚实口径不画占位）。错误口径：仅 404 归 notFound，403/网络/5xx 走 StateBlock 错误态 + 重试。
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import StatusBadge from '@/components/base/StatusBadge.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import AttachmentList from '@/components/business/AttachmentList.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { useMobile } from '@/composables/useMobile'
import { formatDateTime } from '@/utils/datetime'
import { logCategoryKey, logSourceKey } from '@/utils/logMeta'
import { resolveLocalizedText } from '@/utils/contentLanguage'
import { getLog, type LogItem } from '@/api/logs'
import { useProjectStore } from '@/stores/project'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const isMobile = useMobile()
const projects = useProjectStore()

const logId = computed(() => String(route.params.id || ''))

const { data: log, loading, error, run } = useAsyncData<LogItem>(
  async () => {
    // 项目名展示依赖项目列表缓存；加载失败不阻塞日志本体渲染（回退显示 ID）
    try {
      await projects.load()
    } catch {
      // 项目列表失败仅影响项目名展示
    }
    return getLog(logId.value)
  },
  { watch: [logId] }
)

const notFound = computed(() => error.value?.kind === 'not_found')

const projectName = computed(() => {
  const id = log.value?.project_id || ''
  return projects.projects.find((p) => p.id === id)?.name || id
})

const contentLocalized = computed(() =>
  resolveLocalizedText(log.value?.content || '', log.value?.translations?.content, locale.value)
)

function categoryLabel(c: string) {
  const key = logCategoryKey(c)
  return key ? t(key) : c
}

function sourceLabel(s: string) {
  const key = logSourceKey(s)
  return key ? t(key) : s
}

function goBack() {
  if (window.history.length > 1) router.back()
  else if (log.value?.project_id) router.push(`/projects/${log.value.project_id}/logs`)
  else router.push('/projects')
}

function goProject() {
  if (log.value?.project_id) router.push(`/projects/${log.value.project_id}/logs`)
}
</script>

<style scoped>
.panel {
  align-content: start;
  display: grid;
  gap: 14px;
}

.panel h3 {
  color: var(--text-1);
  font-size: 15px;
}

.log-content {
  color: var(--text-2);
  line-height: 1.7;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.raw-snippet {
  color: var(--text-3);
  font-size: 13px;
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
</style>
