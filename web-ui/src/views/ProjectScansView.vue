<template>
  <div class="page">
    <section class="panel">
      <header class="scans-head">
        <div>
          <h3>{{ t('scan.title') }}</h3>
          <p class="hint">{{ t('scan.subtitle') }}</p>
        </div>
        <el-button type="primary" @click="openUpload">{{ t('scan.upload') }}</el-button>
      </header>

      <div class="filters">
        <el-input v-model="q" clearable :placeholder="t('scan.filterQ')" @keyup.enter="applyFilters" />
        <el-input v-model="tags" clearable :placeholder="t('scan.filterTags')" @keyup.enter="applyFilters" />
        <el-select v-model="annotated" clearable :placeholder="t('scan.filterAnnotated')">
          <el-option :label="t('scan.annotatedAll')" value="" />
          <el-option :label="t('scan.annotatedYes')" value="true" />
          <el-option :label="t('scan.annotatedNo')" value="false" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          value-format="YYYY-MM-DD"
          :start-placeholder="t('scan.dateFrom')"
          :end-placeholder="t('scan.dateTo')"
        />
        <el-button @click="applyFilters">{{ t('scan.search') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="items" class="scan-table" @row-click="openDetail">
        <el-table-column :label="t('scan.scanTime')" width="165">
          <template #default="{ row }">{{ formatDateTime(row.scan_at || row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('scan.file')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ row.filename }}</span>
            <el-tag v-if="row.is_void" type="info" size="small" class="ml">{{ t('scan.voided') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('scan.format')" width="90">
          <template #default="{ row }">{{ row.format }}</template>
        </el-table-column>
        <el-table-column :label="t('scan.rows')" width="90">
          <template #default="{ row }">{{ row.row_count }}</template>
        </el-table-column>
        <el-table-column :label="t('scan.tags')" min-width="160">
          <template #default="{ row }">
            <el-tag v-for="tag in row.tags" :key="tag" size="small" class="ml">{{ tag }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('scan.description')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.title || row.description || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('scan.actions')" width="180">
          <template #default="{ row }">
            <el-button link type="primary" @click.stop="openDetail(row)">{{ t('scan.detail') }}</el-button>
            <el-button link @click.stop="download(row, 'txt')">{{ t('scan.downloadTxt') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('scan.empty')" />
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="perPage"
          layout="total, sizes, prev, pager, next"
          :page-sizes="[20, 50, 100]"
          :total="total"
          @current-change="load"
          @size-change="onSizeChange"
        />
      </div>
      <p v-if="error" class="error">{{ error }}</p>
    </section>

    <el-drawer v-model="detailOpen" :title="selected?.filename || t('scan.detail')" size="680px">
      <div v-if="selected" v-loading="detailLoading" class="detail">
        <div class="detail-meta">
          <span>{{ t('scan.machine') }}: {{ selected.machine }}</span>
          <span>{{ t('scan.format') }}: {{ selected.format }}</span>
          <span>{{ t('scan.rows') }}: {{ selected.row_count }} / {{ t('scan.badRows') }}: {{ selected.bad_rows }}</span>
          <span>{{ t('scan.scanTime') }}: {{ formatDateTime(selected.scan_at || selected.created_at) }}</span>
        </div>
        <div class="detail-image">
          <el-image
            v-if="selected.png_path"
            :src="scanFileUrl(selected.id, 'png')"
            :preview-src-list="[scanFileUrl(selected.id, 'png')]"
            fit="contain"
            class="preview"
          />
          <el-empty v-else :description="t('scan.noImage')" />
        </div>
        <el-form label-width="90px">
          <el-form-item :label="t('scan.titleLabel')">
            <el-input v-model="editTitle" maxlength="200" show-word-limit :disabled="selected.is_void" />
          </el-form-item>
          <el-form-item :label="t('scan.description')">
            <el-input v-model="editDescription" type="textarea" :rows="5" maxlength="20000" :disabled="selected.is_void" />
          </el-form-item>
          <el-form-item :label="t('scan.tags')">
            <el-select v-model="editTags" multiple filterable allow-create default-first-option :disabled="selected.is_void" />
          </el-form-item>
        </el-form>
        <div class="detail-actions">
          <el-button type="primary" :disabled="selected.is_void" :loading="saving" @click="saveDetail">{{ t('scan.save') }}</el-button>
          <el-button @click="download(selected, 'txt')">{{ t('scan.downloadTxt') }}</el-button>
          <el-button v-if="selected.png_path" @click="download(selected, 'png')">{{ t('scan.downloadPng') }}</el-button>
          <el-button v-if="!selected.is_void" type="danger" plain @click="voidSelected">{{ t('scan.void') }}</el-button>
        </div>
        <p v-if="selected.parse_warning" class="warning">{{ selected.parse_warning }}</p>
      </div>
    </el-drawer>

    <el-dialog v-model="uploadOpen" :title="t('scan.uploadTitle')" width="560px">
      <el-form label-width="90px">
        <el-form-item :label="t('scan.txtFile')" required>
          <input type="file" accept=".txt,text/plain" @change="onTxtChange" />
          <span v-if="txtFile" class="file-name">{{ txtFile.name }}</span>
        </el-form-item>
        <el-form-item :label="t('scan.pngFile')">
          <input type="file" accept=".png,image/png" @change="onPngChange" />
          <span v-if="pngFile" class="file-name">{{ pngFile.name }}</span>
        </el-form-item>
        <el-form-item :label="t('scan.tags')">
          <el-input v-model="uploadTags" :placeholder="t('scan.tagPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('scan.titleLabel')">
          <el-input v-model="uploadTitle" maxlength="200" />
        </el-form-item>
        <el-form-item :label="t('scan.description')">
          <el-input v-model="uploadDescription" type="textarea" :rows="3" maxlength="20000" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadOpen = false">{{ t('scan.cancel') }}</el-button>
        <el-button type="primary" :loading="uploading" :disabled="!txtFile" @click="submitUpload">{{ t('scan.upload') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  downloadScanFile, getScan, listScans, scanFileUrl,
  updateScan, uploadScan, voidScan, type ScanItem,
} from '@/api/scans'
import { formatDateTime } from '@/utils/datetime'

const route = useRoute()
const { t } = useI18n()
const projectId = () => String(route.params.id || '')

const items = ref<ScanItem[]>([])
const total = ref(0)
const page = ref(1)
const perPage = ref(20)
const loading = ref(false)
const error = ref('')
const q = ref('')
const tags = ref('')
const annotated = ref('')
const dateRange = ref<[string, string] | null>(null)

const detailOpen = ref(false)
const detailLoading = ref(false)
const selected = ref<ScanItem | null>(null)
const editTitle = ref('')
const editDescription = ref('')
const editTags = ref<string[]>([])
const saving = ref(false)

const uploadOpen = ref(false)
const uploading = ref(false)
const txtFile = ref<File | null>(null)
const pngFile = ref<File | null>(null)
const uploadTags = ref('')
const uploadTitle = ref('')
const uploadDescription = ref('')

async function load() {
  if (!projectId()) return
  loading.value = true
  error.value = ''
  try {
    const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
    if (q.value) params.q = q.value
    if (tags.value) params.tags = tags.value
    if (annotated.value) params.annotated = annotated.value
    if (dateRange.value?.[0]) params.from = dateRange.value[0]
    if (dateRange.value?.[1]) params.to = dateRange.value[1]
    const result = await listScans(projectId(), params)
    items.value = result.items ?? []
    total.value = result.total ?? 0
  } catch {
    error.value = t('scan.loadFailed')
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 1
  load()
}

function onSizeChange(size: number) {
  perPage.value = size
  page.value = 1
  load()
}

async function openDetail(row: ScanItem) {
  detailOpen.value = true
  detailLoading.value = true
  try {
    selected.value = await getScan(row.id)
    editTitle.value = selected.value.title
    editDescription.value = selected.value.description
    editTags.value = [...selected.value.tags]
  } catch {
    ElMessage.error(t('scan.loadFailed'))
  } finally {
    detailLoading.value = false
  }
}

async function saveDetail() {
  if (!selected.value) return
  saving.value = true
  try {
    selected.value = await updateScan(selected.value.id, {
      title: editTitle.value,
      description: editDescription.value,
      tags: editTags.value,
    })
    ElMessage.success(t('scan.saved'))
    await load()
  } catch {
    ElMessage.error(t('scan.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function voidSelected() {
  if (!selected.value) return
  try {
    const { value } = await ElMessageBox.prompt(t('scan.voidReason'), t('scan.void'), {
      confirmButtonText: t('scan.confirm'),
      cancelButtonText: t('scan.cancel'),
      inputValidator: (v: string) => (v && v.trim().length > 0 ? true : t('scan.voidReasonRequired')),
    })
    selected.value = await voidScan(selected.value.id, value)
    ElMessage.success(t('scan.voided'))
    await load()
  } catch {
    // cancelled
  }
}

async function download(row: ScanItem, kind: 'txt' | 'png') {
  try {
    await downloadScanFile(row.id, kind, kind === 'txt' ? row.filename : `${row.filename}.png`)
  } catch {
    ElMessage.error(t('scan.downloadFailed'))
  }
}

function openUpload() {
  txtFile.value = null
  pngFile.value = null
  uploadTags.value = ''
  uploadTitle.value = ''
  uploadDescription.value = ''
  uploadOpen.value = true
}

function onTxtChange(event: Event) {
  txtFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
}

function onPngChange(event: Event) {
  pngFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
}

async function submitUpload() {
  if (!txtFile.value || !projectId()) return
  uploading.value = true
  try {
    const form = new FormData()
    form.append('txt', txtFile.value)
    if (pngFile.value) form.append('png', pngFile.value)
    if (uploadTags.value) form.append('tags', uploadTags.value)
    if (uploadTitle.value) form.append('title', uploadTitle.value)
    if (uploadDescription.value) form.append('description', uploadDescription.value)
    const result = await uploadScan(projectId(), form)
    ElMessage.success(result.duplicate ? t('scan.duplicate') : t('scan.uploaded'))
    uploadOpen.value = false
    await load()
  } catch {
    ElMessage.error(t('scan.uploadFailed'))
  } finally {
    uploading.value = false
  }
}

onMounted(() => {
  load()
})
</script>

<style scoped>
.scans-head {
  align-items: flex-start;
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
}

.hint {
  color: var(--text-3);
  font-size: var(--fs-sm);
  margin-top: 4px;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: var(--space-3) 0;
}

.filters :deep(.el-input),
.filters :deep(.el-select) {
  max-width: 220px;
}

.scan-table {
  width: 100%;
}

.ml {
  margin-left: 4px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-3);
}

.error {
  color: var(--danger-text);
  margin-top: var(--space-2);
}

.detail {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.detail-meta {
  color: var(--text-3);
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  font-size: var(--fs-sm);
}

.detail-image {
  align-items: center;
  background: var(--surface-2);
  display: flex;
  justify-content: center;
  min-height: 200px;
}

.preview {
  max-height: 360px;
  max-width: 100%;
}

.detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.warning {
  color: var(--warn-text);
  font-size: var(--fs-body-sm);
}

.file-name {
  color: var(--text-3);
  font-size: var(--fs-body-sm);
  margin-left: var(--space-2);
}
</style>
