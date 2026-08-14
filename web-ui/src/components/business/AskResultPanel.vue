<template>
  <div class="result-panel">
    <div v-if="data.answer" class="answer-box">
      <MarkdownView :source="data.answer" />
    </div>
    <template v-if="data.rows && data.rows.length > 0">
      <div class="sql-box">
        <details>
          <summary>{{ t('ask.viewSql') }}</summary>
          <pre class="sql-text">{{ data.sql }}</pre>
        </details>
      </div>
      <div class="panel data-panel">
        <div class="data-head">
          <strong class="table-name">{{ data.tableName || '-' }}</strong>
          <span class="muted">{{ t('ask.rowCount', { n: data.rowCount }) }}</span>
          <span v-if="data.durationMs" class="muted">{{ t('ask.duration', { ms: data.durationMs }) }}</span>
          <el-tag v-if="data.truncated" size="small" type="warning" effect="light">{{ t('ask.truncated') }}</el-tag>
        </div>
        <el-table :data="data.rows" size="small" :max-height="360" class="data-table">
          <el-table-column
            v-for="col in data.columns"
            :key="col"
            :prop="col"
            :label="col"
            min-width="140"
            show-overflow-tooltip
          />
          <el-table-column v-if="showDetailColumn" :label="t('ask.detail')" width="112" fixed="right" align="center">
            <template #default="{ row }">
              <el-button
                v-if="canOpenRow(row, data.tableName)"
                link
                type="primary"
                size="small"
                @click="open(row)"
              >
                {{ t('ask.viewDetail') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import MarkdownView from '@/components/business/MarkdownView.vue'
import { canOpenRow, hasRowRoute, tableToRoute } from '@/composables/askRoutes'

export type AskResultData = {
  answer: string
  sql: string
  tableName: string
  columns: string[]
  rows: Record<string, unknown>[]
  rowCount: number
  truncated: boolean
  durationMs: number
}

const props = defineProps<{ data: AskResultData }>()

const emit = defineEmits<{ open: [route: string] }>()

const { t } = useI18n()

const showDetailColumn = computed(() => hasRowRoute(props.data.tableName))

function open(row: Record<string, unknown>) {
  const route = tableToRoute(row, props.data.tableName)
  if (route) emit('open', route)
}
</script>

<style scoped>
.result-panel {
  display: grid;
  gap: 12px;
}

.answer-box {
  color: var(--text-2);
}

.sql-box {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.sql-box summary {
  color: var(--text-3);
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
  padding: 8px 12px;
  user-select: none;
}

.sql-text {
  background: var(--surface-2);
  border-top: 1px solid var(--border);
  color: var(--text-2);
  font-size: 12px;
  line-height: 1.6;
  margin: 0;
  max-height: 200px;
  overflow: auto;
  padding: 10px 12px;
  white-space: pre-wrap;
  word-break: break-all;
}

.data-panel {
  padding: 14px;
}

.data-head {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 10px;
}

.table-name {
  color: var(--text-1);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
}

.data-table {
  width: 100%;
}
</style>
