<template>
  <div class="page">
    <div class="toolbar">
      <h2>附件管理</h2>
    </div>
    <section v-if="canOperate" class="panel upload-panel">
      <el-upload drag multiple :show-file-list="false" :http-request="onUpload" class="uploader">
        <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
        <div class="el-upload__text">将文件拖到此处，或<em>点击上传</em></div>
      </el-upload>
      <div class="bind-form">
        <p class="muted">不填写绑定信息则上传为未绑定附件</p>
        <el-select v-model="bindForm.entity_type" clearable placeholder="绑定对象类型">
          <el-option v-for="t in ATTACHMENT_ENTITY_TYPES" :key="t" :label="t" :value="t" />
        </el-select>
        <el-input v-model="bindForm.entity_id" placeholder="绑定对象 ID" clearable />
        <el-input v-model="bindForm.description" placeholder="附件描述" clearable />
      </div>
    </section>
    <section class="panel filters-panel">
      <div class="filters">
        <el-select v-model="filterType" clearable placeholder="绑定对象类型">
          <el-option v-for="t in ATTACHMENT_ENTITY_TYPES" :key="t" :label="t" :value="t" />
        </el-select>
        <el-input v-model="filterEntityId" placeholder="绑定对象 ID" clearable />
        <el-button type="primary" @click="search">查询</el-button>
      </div>
      <p class="muted filter-hint">两个条件都留空时显示未绑定附件列表</p>
    </section>
    <section class="panel">
      <el-alert v-if="loadError" class="load-error" type="error" :title="loadError" show-icon :closable="false">
        <el-button size="small" @click="load">重试</el-button>
      </el-alert>
      <div v-loading="loading" class="list-area">
        <div v-if="items.length" class="card-grid">
          <div v-for="att in items" :key="att.id" class="att-card">
            <div class="thumb">
              <img v-if="thumbUrls[att.id]" :src="thumbUrls[att.id]" :alt="att.original_name" />
              <el-icon v-else :size="40"><Document /></el-icon>
            </div>
            <el-tooltip :content="att.original_name" placement="top" :show-after="300">
              <p class="att-name">{{ att.original_name }}</p>
            </el-tooltip>
            <p class="att-meta">{{ fmtSize(att.file_size) }} · {{ fmtTime(att.created_at) }}</p>
            <p v-if="att.description" class="att-desc">{{ att.description }}</p>
            <div class="att-actions">
              <el-button size="small" @click="download(att)">下载</el-button>
              <template v-if="canOperate">
                <el-button size="small" type="primary" plain @click="openBind(att)">绑定</el-button>
                <el-button size="small" type="danger" plain @click="remove(att)">删除</el-button>
              </template>
            </div>
          </div>
        </div>
        <el-empty v-if="!loading && !loadError && items.length === 0" description="暂无附件" />
        <el-pagination
          v-if="total > 0"
          v-model:current-page="page"
          class="pager"
          layout="total, prev, pager, next"
          :page-size="pageSize"
          :total="total"
          @current-change="load"
        />
      </div>
    </section>
    <el-dialog v-model="bindDialog" title="绑定附件" width="480">
      <el-form label-position="top">
        <el-form-item label="绑定对象类型" required>
          <el-select v-model="linkForm.entity_type" placeholder="选择类型">
            <el-option v-for="t in ATTACHMENT_ENTITY_TYPES" :key="t" :label="t" :value="t" />
          </el-select>
        </el-form-item>
        <el-form-item label="绑定对象 ID" required>
          <el-input v-model="linkForm.entity_id" placeholder="对象 UUID" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="linkForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="bindDialog = false">取消</el-button>
        <el-button type="primary" @click="submitBind">绑定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
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
} from '../api/attachments'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const auth = useAuthStore()
const items = ref<Attachment[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 24
const loading = ref(false)
const loadError = ref('')
const filterType = ref('')
const filterEntityId = ref('')
const bindDialog = ref(false)
const bindTarget = ref<Attachment | null>(null)
const bindForm = reactive({ entity_type: '', entity_id: '', description: '' })
const linkForm = reactive({ entity_type: '', entity_id: '', description: '' })
const thumbUrls = ref<Record<string, string>>({})
const createdUrls: string[] = []

const canOperate = computed(() => ['admin', 'maintainer', 'member'].includes(auth.user?.role || ''))

onMounted(load)
onBeforeUnmount(() => {
  for (const url of createdUrls) URL.revokeObjectURL(url)
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    // entity_type/entity_id 必须成对出现；都空 = 未绑定附件列表
    const params: Record<string, string | number> = { page: page.value, per_page: pageSize }
    if (filterType.value && filterEntityId.value.trim()) {
      params.entity_type = filterType.value
      params.entity_id = filterEntityId.value.trim()
    }
    const data = await listAttachments(params)
    items.value = data.items ?? []
    total.value = data.total
    await loadThumbs()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : '附件加载失败'
  } finally {
    loading.value = false
  }
}

// 为图片附件加载缩略图，失败时回退到文件图标
async function loadThumbs() {
  for (const url of createdUrls) URL.revokeObjectURL(url)
  createdUrls.length = 0
  const map: Record<string, string> = {}
  await Promise.all(
    items.value
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
    ElMessage.warning('绑定对象类型和 ID 需要同时填写或同时留空')
    return
  }
  page.value = 1
  load()
}

function pairInvalid(entityType: string, entityId: string) {
  return !entityType !== !entityId.trim()
}

async function onUpload(options: UploadRequestOptions) {
  if (pairInvalid(bindForm.entity_type, bindForm.entity_id)) {
    ElMessage.warning('绑定对象类型和 ID 需要同时填写或同时留空')
    return
  }
  try {
    await uploadAttachment(options.file, bindForm.entity_type, bindForm.entity_id.trim(), bindForm.description.trim())
    ElMessage.success('上传成功')
    await load()
  } catch (err) {
    showApiError(err, '上传失败')
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
    showApiError(err, '下载失败')
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
    ElMessage.warning('请填写绑定对象类型和 ID')
    return
  }
  try {
    await addAttachmentLink(bindTarget.value.id, {
      entity_type: linkForm.entity_type,
      entity_id: linkForm.entity_id.trim(),
      description: linkForm.description.trim() || undefined
    })
    bindDialog.value = false
    ElMessage.success('绑定成功')
    await load()
  } catch (err) {
    showApiError(err, '绑定失败')
  }
}

async function remove(att: Attachment) {
  try {
    await ElMessageBox.confirm(`确认删除附件「${att.original_name}」？`, '删除附件', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteAttachment(att.id)
    ElMessage.success('附件已删除')
    await load()
  } catch (err) {
    showApiError(err, '删除失败')
  }
}

function fmtSize(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function fmtTime(t?: string) {
  return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—'
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

.load-error {
  margin-bottom: 16px;
}

.list-area {
  min-height: 120px;
}

.card-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
}

.att-card {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 10px;
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
  background: #fff;
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
