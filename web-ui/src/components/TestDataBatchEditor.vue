<template>
  <div class="batch-editor">
    <el-alert v-if="summaryText" :title="summaryText" type="error" show-icon :closable="false" class="err-summary">
      <ul class="err-list">
        <li v-for="(item, i) in summaryItems" :key="i">
          {{ t('testData.rowFieldError', { row: item.row, field: item.field, message: item.message }) }}
        </li>
      </ul>
    </el-alert>

    <div class="toolbar">
      <el-button size="small" :disabled="rows.length >= MAX_ROWS || submitting" @click="addRow">
        {{ t('testData.addRow') }}
      </el-button>
      <el-button size="small" :disabled="rows.length === 0 || submitting" @click="clearRows">
        {{ t('testData.clearRows') }}
      </el-button>
      <el-button size="small" :disabled="submitting" @click="pasteFromClipboard">
        {{ t('testData.paste') }}
      </el-button>
      <div class="spacer" />
      <el-button type="primary" size="small" :loading="submitting" @click="submit">
        {{ t('testData.submitBatch', { n: rows.length }) }}
      </el-button>
    </div>

    <div class="table-scroll">
      <el-table
        :data="rows"
        size="small"
        :row-key="rowKey"
        :row-class-name="rowClassName"
        :cell-class-name="cellClassName"
      >
        <el-table-column :label="t('testData.rowNoLabel')" width="70" align="center">
          <template #default="{ $index }">{{ t('testData.rowNo', { n: $index + 1 }) }}</template>
        </el-table-column>
        <el-table-column prop="data_type" :label="t('testData.dataType')" width="130">
          <template #default="{ row }">
            <el-select v-model="row.data_type" size="small" :placeholder="t('testData.dataTypePlaceholder')">
              <el-option v-for="d in dataTypes" :key="d" :label="d" :value="d" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column prop="measurement" :label="t('testData.measurement')" min-width="150">
          <template #default="{ row }">
            <el-input v-model="row.measurement" size="small" :placeholder="t('testData.measurementPlaceholder')" />
          </template>
        </el-table-column>
        <el-table-column prop="value" :label="t('testData.value')" width="130">
          <template #default="{ row }">
            <el-input-number v-model="row.value" size="small" :controls="false" class="value-input" />
          </template>
        </el-table-column>
        <el-table-column prop="unit" :label="t('testData.unit')" width="100">
          <template #default="{ row }">
            <el-input v-model="row.unit" size="small" :placeholder="t('testData.unitPlaceholder')" />
          </template>
        </el-table-column>
        <el-table-column prop="quality" :label="t('testData.quality')" width="120">
          <template #default="{ row }">
            <el-select v-model="row.quality" size="small">
              <el-option v-for="q in entryQualities" :key="q" :label="q" :value="q" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column prop="measured_at" :label="t('testData.measuredAt')" width="180">
          <template #default="{ row }">
            <el-date-picker
              v-model="row.measured_at"
              size="small"
              type="datetime"
              :placeholder="t('testData.timePlaceholder')"
            />
          </template>
        </el-table-column>
        <el-table-column prop="run_id" :label="t('testData.linkedRun')" width="200">
          <template #default="{ row }">
            <el-select v-model="row.run_id" size="small" clearable :placeholder="t('testData.runPlaceholder')">
              <el-option v-for="r in runs" :key="r.id" :label="r.name" :value="r.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column prop="notes" :label="t('testData.notes')" min-width="140">
          <template #default="{ row }">
            <el-input v-model="row.notes" size="small" :placeholder="t('testData.notesPlaceholder')" />
          </template>
        </el-table-column>
        <el-table-column :label="t('testData.actions')" width="80" align="center">
          <template #default="{ $index }">
            <el-button size="small" text type="danger" @click="removeRow($index)">{{ t('testData.deleteRow') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('testData.emptyRows')" :image-size="48" />
        </template>
      </el-table>
    </div>

    <el-dialog v-model="pasteModalVisible" :title="t('testData.pasteModalTitle')" width="560px">
      <p class="paste-hint">{{ t('testData.pasteFallback') }}</p>
      <el-input
        v-model="pasteText"
        type="textarea"
        :rows="10"
        :placeholder="t('testData.pastePlaceholder')"
      />
      <template #footer>
        <el-button @click="pasteModalVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="applyPaste(pasteText)">{{ t('testData.parse') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { createTestDataBatch, type TestDataBatchRow } from '../api/testdata'
import type { ExperimentRun } from '../api/runs'
import { showApiError } from '../composables/useNotify'
import {
  MAX_BATCH_ROWS,
  parsePastedTestData,
  validateRows,
  type BatchRow,
  type ClientErrorCode,
  type RowApiError
} from '../utils/testDataPaste'

const props = defineProps<{
  projectId: string
  runs: ExperimentRun[]
}>()

const emit = defineEmits<{ submitted: [] }>()

const { t } = useI18n()

const MAX_ROWS = MAX_BATCH_ROWS
const dataTypes = ['cryo', 'pressure', 'voltage', 'rf_voltage', 'efficiency']
const entryQualities = ['normal', 'outlier', 'suspect']

type EditableRow = BatchRow & { _key: number }

let keySeq = 0
const rows = ref<EditableRow[]>([])
const submitting = ref(false)
const pasteModalVisible = ref(false)
const pasteText = ref('')

// 当前错误标记：rowIndex → field → 可展示消息（客户端校验或后端 422 来源共用）
const errorsMap = ref(new Map<number, Map<string, string>>())
const summaryText = ref('')

function newBlankRow(): EditableRow {
  return {
    _key: ++keySeq,
    data_type: '',
    measurement: '',
    value: undefined,
    unit: '',
    quality: 'normal',
    measured_at: undefined,
    run_id: undefined,
    notes: ''
  }
}

rows.value.push(newBlankRow())

// 行内编辑任何变更、新增/删除/清空行、粘贴解析 → 清除当前错误标记（行序已变，后端 index 不再可映射）
watch(rows, () => clearErrors(), { deep: true })

function clearErrors() {
  errorsMap.value = new Map()
  summaryText.value = ''
}

const summaryItems = computed(() => {
  const items: { row: number; field: string; message: string }[] = []
  const indices = [...errorsMap.value.keys()].sort((a, b) => a - b)
  for (const index of indices) {
    const fields = errorsMap.value.get(index)
    if (!fields) continue
    for (const [field, message] of fields) {
      items.push({ row: index + 1, field: fieldLabel(field), message })
      if (items.length >= 5) return items
    }
  }
  return items
})

function fieldLabel(field: string): string {
  const labels: Record<string, string> = {
    data_type: t('testData.dataType'),
    measurement: t('testData.measurement'),
    value: t('testData.value'),
    unit: t('testData.unit'),
    quality: t('testData.quality'),
    measured_at: t('testData.measuredAt'),
    run_id: t('testData.linkedRun'),
    notes: t('testData.notes')
  }
  return labels[field] ?? field
}

function clientMessage(code: ClientErrorCode): string {
  const messages: Record<ClientErrorCode, string> = {
    required: t('testData.clientRequired'),
    not_a_number: t('testData.valueNotNumber'),
    not_a_date: t('testData.dateNotValid'),
    invalid_enum: t('testData.clientInvalidEnum'),
    too_long: t('testData.clientTooLong'),
    invalid_uuid: t('testData.clientInvalidUuid')
  }
  return messages[code] ?? code
}

function rowKey(row: EditableRow) {
  return row._key
}

function cellClassName({ rowIndex, column }: { rowIndex: number; column: { property?: string } }) {
  if (!column.property) return ''
  return errorsMap.value.get(rowIndex)?.has(column.property) ? 'td-error' : ''
}

function rowClassName({ rowIndex }: { rowIndex: number }) {
  return errorsMap.value.has(rowIndex) ? 'row-error' : ''
}

function addRow() {
  if (rows.value.length >= MAX_ROWS) {
    ElMessage.warning(t('testData.batchTooLarge'))
    return
  }
  rows.value.push(newBlankRow())
}

function removeRow(index: number) {
  rows.value.splice(index, 1)
}

function clearRows() {
  rows.value = [newBlankRow()]
}

async function pasteFromClipboard() {
  let text = ''
  if (typeof navigator !== 'undefined' && navigator.clipboard?.readText) {
    try {
      text = await navigator.clipboard.readText()
    } catch {
      // 内网 HTTP 部署/权限拒绝 → 走降级弹窗
      text = ''
    }
  }
  if (!text.trim()) {
    // 打开弹窗时清空旧文本，避免残留上一次粘贴内容；弹窗内已有引导文案，不再重复 toast
    pasteText.value = ''
    pasteModalVisible.value = true
    return
  }
  applyPaste(text)
}

function applyPaste(text: string) {
  const result = parsePastedTestData(text)
  pasteModalVisible.value = false
  if (result.rows.length === 0) {
    ElMessage.warning(t('testData.pasteNoData'))
    return
  }
  // 追加语义：解析结果追加到现有表格行之后（多次粘贴可累加）；按总行数上限截断，避免 60 行时再粘 100 行变成死胡同
  const capacity = MAX_ROWS - rows.value.length
  if (capacity <= 0) {
    ElMessage.warning(t('testData.batchTooLarge'))
    return
  }
  const accepted = result.rows.slice(0, capacity)
  rows.value.push(...accepted.map((r) => ({ ...r, _key: ++keySeq })))
  if (result.truncated || accepted.length < result.rows.length) {
    ElMessage.warning(t('testData.parseTruncated'))
  } else {
    ElMessage.success(t('testData.parseOk', { n: accepted.length }))
  }
}

async function submit() {
  if (submitting.value) return
  if (rows.value.length === 0) {
    ElMessage.warning(t('testData.emptyRows'))
    return
  }
  if (rows.value.length > MAX_ROWS) {
    ElMessage.warning(t('testData.batchTooLarge'))
    return
  }
  // 客户端校验（规则与后端一致）：有错 → 标红 + 提示，不发请求
  const clientErrors = validateRows(rows.value)
  if (clientErrors.size > 0) {
    const map = new Map<number, Map<string, string>>()
    for (const [index, fields] of clientErrors) {
      const messages = new Map<string, string>()
      for (const [field, codes] of fields) {
        messages.set(field, [...codes].map((code) => clientMessage(code)).join(' / '))
      }
      map.set(index, messages)
    }
    errorsMap.value = map
    summaryText.value = t('testData.clientInvalidRows', { n: clientErrors.size })
    ElMessage.warning(t('testData.clientInvalidRows', { n: clientErrors.size }))
    return
  }
  // 只提交非空可选字段（与单条 submit 的 payload 构建一致）；Idempotency-Key 由 client.ts 自动注入
  const payload: TestDataBatchRow[] = rows.value.map((r) => {
    const item: TestDataBatchRow = {
      data_type: r.data_type,
      measurement: r.measurement.trim(),
      // el-input-number 清空 emit null：转回 undefined，避免 payload 出现 null 值
      value: r.value ?? undefined
    }
    const unit = r.unit.trim()
    if (unit) item.unit = unit
    if (r.quality) item.quality = r.quality
    if (r.measured_at && !Number.isNaN(r.measured_at.getTime())) {
      item.measured_at = new Date(r.measured_at).toISOString()
    }
    const runId = r.run_id?.trim()
    if (runId) item.run_id = runId
    const notes = r.notes.trim()
    if (notes) item.notes = notes
    return item
  })
  submitting.value = true
  try {
    const res = await createTestDataBatch(props.projectId, payload)
    ElMessage.success(t('testData.batchCreated', { n: res.count }))
    clearErrors()
    // 清空全部行 + 保留一行空白行
    rows.value = [newBlankRow()]
    emit('submitted')
  } catch (err) {
    const typed = err as Error & { details?: { errors?: RowApiError[] } }
    if (typed?.details?.errors?.length) {
      const map = new Map<number, Map<string, string>>()
      for (const e of typed.details.errors) {
        let fields = map.get(e.index)
        if (!fields) {
          fields = new Map()
          map.set(e.index, fields)
        }
        fields.set(e.field, e.message)
      }
      errorsMap.value = map
      summaryText.value = t('testData.serverInvalidRows', { n: typed.details.errors.length })
      ElMessage.error(t('testData.serverInvalidRows', { n: typed.details.errors.length }))
    } else {
      showApiError(err, t('testData.createFailed'))
    }
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.batch-editor {
  display: grid;
  gap: 12px;
}

.err-summary :deep(.el-alert__content) {
  width: 100%;
}

.err-list {
  margin: 6px 0 0;
  padding-left: 18px;
  color: var(--danger);
  font-size: 13px;
}

.toolbar {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.spacer {
  flex: 1;
}

.table-scroll {
  max-width: 100%;
  overflow-x: auto;
}

.table-scroll :deep(.el-table .td-error) {
  background: #fbe9e7;
}

.table-scroll :deep(.el-table .row-error > td.el-table__cell) {
  background: #fbe9e7;
}

.table-scroll :deep(.el-table .row-error > td.el-table__cell .cell) {
  color: var(--danger);
}

.table-scroll :deep(.el-input-number.value-input) {
  width: 100%;
}

.table-scroll :deep(.el-select) {
  width: 100%;
}

.table-scroll :deep(.el-date-editor) {
  width: 100%;
}

.paste-hint {
  color: var(--text-3);
  font-size: 13px;
  margin: 0 0 8px;
}
</style>
