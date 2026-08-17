<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('attachment.title') }}</h2>
    </div>
    <section v-if="canOperate" class="panel upload-panel">
      <el-upload drag multiple :show-file-list="false" :http-request="onUpload" class="uploader">
        <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
        <div class="el-upload__text" v-html="t('attachment.dragOrClick')"></div>
      </el-upload>
      <div class="bind-form">
        <p class="muted">{{ t('attachment.uploadHint') }}</p>
        <el-select v-model="bindForm.entity_type" clearable :placeholder="t('attachment.entityTypePlaceholder')">
          <el-option v-for="t in ATTACHMENT_ENTITY_TYPES" :key="t" :label="t" :value="t" />
        </el-select>
        <el-input v-model="bindForm.entity_id" :placeholder="t('attachment.entityIdPlaceholder')" clearable />
        <el-input v-model="bindForm.description" :placeholder="t('attachment.descriptionPlaceholder')" clearable />
      </div>
    </section>
    <section class="panel filters-panel">
      <div class="filters">
        <el-select v-model="filterType" clearable :placeholder="t('attachment.entityTypePlaceholder')">
          <el-option v-for="t in ATTACHMENT_ENTITY_TYPES" :key="t" :label="t" :value="t" />
        </el-select>
        <el-input v-model="filterEntityId" :placeholder="t('attachment.entityIdPlaceholder')" clearable />
        <el-button type="primary" @click="search">{{ t('attachment.search') }}</el-button>
      </div>
      <p class="muted filter-hint">{{ t('attachment.filterHint') }}</p>
    </section>
    <section class="panel">
      <!-- 列表三态收敛 StateBlock：首屏骨架（翻页有数据时不闪骨架）> 错误 > 空态 > 卡片网格 -->
      <StateBlock
        :loading="loading && !data"
        :error="error"
        :error-text="t('attachment.loadFailed')"
        :empty="items.length === 0"
        :empty-text="t('attachment.empty')"
        @retry="run"
      >
        <div v-loading="loading" class="list-area">
          <CardGrid v-if="items.length" min="200px">
            <div v-for="att in items" :key="att.id" class="att-card">
              <div class="thumb">
                <img v-if="thumbUrls[att.id]" :src="thumbUrls[att.id]" :alt="att.original_name" />
                <el-icon v-else :size="40"><Document /></el-icon>
              </div>
              <el-tooltip :content="att.original_name" placement="top" :show-after="300">
                <p class="att-name">{{ att.original_name }}</p>
              </el-tooltip>
              <p class="att-meta">{{ fmtSize(att.file_size) }} · {{ formatDateTime(att.created_at) }}</p>
              <p v-if="att.description" class="att-desc">{{ att.description }}</p>
              <div class="att-actions">
                <el-button size="small" @click="download(att)">{{ t('attachment.download') }}</el-button>
                <template v-if="canOperate">
                  <el-button size="small" type="primary" plain @click="openBind(att)">{{ t('attachment.bind') }}</el-button>
                  <el-button size="small" type="danger" plain @click="remove(att)">{{ t('attachment.delete') }}</el-button>
                </template>
              </div>
            </div>
          </CardGrid>
          <el-pagination
            v-if="total > 0"
            v-model:current-page="page"
            class="pager"
            layout="total, prev, pager, next"
            :page-size="perPage"
            :total="total"
            @current-change="run"
          />
        </div>
      </StateBlock>
    </section>
    <FormDialog v-model="bindDialog" :title="t('attachment.bindTitle')" width="480" @submit="submitBind">
      <el-form-item :label="t('attachment.bindEntityType')" required>
        <el-select v-model="linkForm.entity_type" :placeholder="t('attachment.selectType')">
          <el-option v-for="t in ATTACHMENT_ENTITY_TYPES" :key="t" :label="t" :value="t" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('attachment.bindEntityId')" required>
        <el-input v-model="linkForm.entity_id" :placeholder="t('attachment.objectUuidPlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('attachment.description')">
        <el-input v-model="linkForm.description" type="textarea" :rows="2" />
      </el-form-item>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type UploadRequestOptions } from 'element-plus'
import { Document, UploadFilled } from '@element-plus/icons-vue'
import {
  ATTACHMENT_ENTITY_TYPES,
  addAttachmentLink,
  deleteAttachment,
  getAttachmentContent,
  listAttachments,
  uploadAttachment,
  type Attachment
} from '@/api/attachments'
import { useAuthStore } from '@/stores/auth'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import StateBlock from '@/components/base/StateBlock.vue'
import CardGrid from '@/components/base/CardGrid.vue'
import FormDialog from '@/components/base/FormDialog.vue'
import { formatDateTime } from '@/utils/datetime'

const auth = useAuthStore()
const { t } = useI18n()
// 分页状态（S4 收敛 usePagination）：perPage 保持原值 24；翻页走 v-model:current-page + @current-change
const { page, perPage, total, setTotal, reset } = usePagination({ perPage: 24 })
const filterType = ref('')
const filterEntityId = ref('')
const bindDialog = ref(false)
const bindTarget = ref<Attachment | null>(null)
const bindForm = reactive({ entity_type: '', entity_id: '', description: '' })
const linkForm = reactive({ entity_type: '', entity_id: '', description: '' })
const thumbUrls = ref<Record<string, string>>({})
const createdUrls: string[] = []

const canOperate = computed(() => ['admin', 'maintainer', 'member'].includes(auth.user?.role || ''))

// 列表加载收敛 useAsyncData（内建竞态/卸载保护，替代手写 loading/loadError/onMounted 三件套）；
// 三态展示由模板 StateBlock 接管，操作后刷新一律走 run()
const { data, loading, error, run } = useAsyncData(loadList)
const items = computed(() => data.value ?? [])

onBeforeUnmount(() => {
  for (const url of createdUrls) URL.revokeObjectURL(url)
})

async function loadList(): Promise<Attachment[]> {
  // entity_type/entity_id 必须成对出现；都空 = 未绑定附件列表
  const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
  if (filterType.value && filterEntityId.value.trim()) {
    params.entity_type = filterType.value
    params.entity_id = filterEntityId.value.trim()
  }
  const res = await listAttachments(params)
  setTotal(res.total)
  const list = res.items ?? []
  await loadThumbs(list)
  return list
}

// 为图片附件加载缩略图，失败时回退到文件图标
async function loadThumbs(list: Attachment[]) {
  for (const url of createdUrls) URL.revokeObjectURL(url)
  createdUrls.length = 0
  const map: Record<string, string> = {}
  await Promise.all(
    list
      .filter((att) => att.mime_type?.startsWith('image/'))
      .map(async (att) => {
        try {
          const blob = await getAttachmentContent(att.id)
          const url = URL.createObjectURL(blob)
          createdUrls.push(url)
          map[att.id] = url
        } catch {
          // 缩略图加载失败，保留默认图标
        }
      })
  )
  thumbUrls.value = map
}

function search() {
  if (pairInvalid(filterType.value, filterEntityId.value)) {
    ElMessage.warning(t('attachment.pairRequired'))
    return
  }
  reset()
  run()
}

function pairInvalid(entityType: string, entityId: string) {
  return !entityType !== !entityId.trim()
}

async function onUpload(options: UploadRequestOptions) {
  if (pairInvalid(bindForm.entity_type, bindForm.entity_id)) {
    ElMessage.warning(t('attachment.pairRequired'))
    return
  }
  try {
    await uploadAttachment(options.file, bindForm.entity_type, bindForm.entity_id.trim(), bindForm.description.trim())
    ElMessage.success(t('attachment.uploadSuccess'))
    await run()
  } catch (err) {
    showApiError(err, t('attachment.uploadFailed'))
  }
}

async function download(att: Attachment) {
  try {
    const blob = await getAttachmentContent(att.id)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = att.original_name
    a.click()
    URL.revokeObjectURL(url)
  } catch (err) {
    showApiError(err, t('attachment.downloadFailed'))
  }
}

function openBind(att: Attachment) {
  bindTarget.value = att
  linkForm.entity_type = ''
  linkForm.entity_id = ''
  linkForm.description = att.description || ''
  bindDialog.value = true
}

async function submitBind() {
  if (!bindTarget.value) return
  if (!linkForm.entity_type || !linkForm.entity_id.trim()) {
    ElMessage.warning(t('attachment.bindRequired'))
    return
  }
  try {
    await addAttachmentLink(bindTarget.value.id, {
      entity_type: linkForm.entity_type,
      entity_id: linkForm.entity_id.trim(),
      description: linkForm.description.trim() || undefined
    })
    bindDialog.value = false
    ElMessage.success(t('attachment.bindSuccess'))
    await run()
  } catch (err) {
    showApiError(err, t('attachment.bindFailed'))
  }
}

async function remove(att: Attachment) {
  try {
    await ElMessageBox.confirm(t('attachment.confirmDelete', { name: att.original_name }), t('attachment.deleteTitle'), { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteAttachment(att.id)
    ElMessage.success(t('attachment.deleted'))
    await run()
  } catch (err) {
    showApiError(err, t('attachment.deleteFailed'))
  }
}

function fmtSize(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}
</script>

<style scoped>
.upload-panel {
  display: grid;
  gap: 16px;
}

.uploader {
  max-width: 520px;
}

.bind-form {
  display: grid;
  gap: 10px;
  max-width: 520px;
}

.filters-panel {
  padding: 14px 20px;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.filters .el-input {
  max-width: 240px;
}

.filters .el-select {
  width: 200px;
}

.filter-hint {
  font-size: 12px;
  margin-top: 8px;
}

.list-area {
  min-height: 120px;
}

/* 网格本体已收口 base/CardGrid（R5）：min 200px + gap --space-4 等值原 .card-grid */

.att-card {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: 8px;
  justify-items: center;
  padding: 14px;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    transform 0.15s ease;
}

.att-card:hover {
  border-color: var(--brand-100);
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.thumb {
  align-items: center;
  /* 缩略图占位底对齐上传/拖拽区规范（美术 §4.5）：surface-2 浅灰底 */
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-3);
  display: flex;
  height: 96px;
  justify-content: center;
  overflow: hidden;
  width: 100%;
}

.thumb img {
  height: 100%;
  object-fit: cover;
  width: 100%;
}

.att-name {
  color: var(--text-1);
  font-size: 13px;
  font-weight: 600;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.att-meta {
  color: var(--text-3);
  font-size: 12px;
}

.att-desc {
  color: var(--text-2);
  font-size: 12px;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.att-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
}

.att-actions .el-button + .el-button {
  margin-left: 0;
}

.pager {
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 768px) {
  .filters .el-input,
  .filters .el-select {
    max-width: none;
    width: 100%;
  }
}
</style>
