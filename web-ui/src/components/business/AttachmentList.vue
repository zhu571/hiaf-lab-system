<template>
  <StateBlock
    :loading="loading && !data"
    :error="error"
    :error-text="t('attachment.loadFailed')"
    :empty="items.length === 0"
    :empty-text="t('attachment.empty')"
    @retry="run"
  >
    <div v-loading="loading" class="attachment-list">
      <div v-for="att in items" :key="att.id" class="att-item">
        <el-image
          v-if="thumbUrls[att.id]"
          class="att-thumb"
          :src="thumbUrls[att.id]"
          :preview-src-list="[thumbUrls[att.id]]"
          :alt="att.original_name"
          fit="cover"
          preview-teleported
        />
        <el-icon v-else class="att-icon" :size="28"><Document /></el-icon>
        <div class="att-info">
          <el-tooltip :content="att.original_name" placement="top" :show-after="300">
            <p class="att-name">{{ att.original_name }}</p>
          </el-tooltip>
          <p class="att-meta">{{ fmtSize(att.file_size) }} · {{ att.mime_type || '—' }}</p>
          <p v-if="att.description" class="att-desc">{{ att.description }}</p>
        </div>
        <el-button size="small" @click="download(att)">{{ t('attachment.download') }}</el-button>
      </div>
    </div>
  </StateBlock>
</template>

<script setup lang="ts">
// 已上传附件展示组件（log-view-optimization 批，业务复合件）：
// 按 entity_type + entity_id 查询服务端已上传附件（listAttachments），图片类加载缩略图并支持
// 点击预览（el-image preview），全部附件提供下载。供日志详情页（entity_type=log）、
// 日报详情页（entity_type=daily_report）等「查看已上传附件/照片」场景复用。
// 只读展示：上传/绑定/删除仍归 AttachmentView 附件管理页，本组件不做写操作。
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Document } from '@element-plus/icons-vue'
import { getAttachmentContent, listAttachments, type Attachment } from '@/api/attachments'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import StateBlock from '@/components/base/StateBlock.vue'

const props = defineProps<{
  entityType: string
  entityId: string
}>()

const { t } = useI18n()

const thumbUrls = ref<Record<string, string>>({})
const createdUrls: string[] = []

const { data, loading, error, run } = useAsyncData<Attachment[]>(loadList, {
  watch: [() => props.entityType, () => props.entityId]
})
const items = computed(() => data.value ?? [])

onBeforeUnmount(() => {
  for (const url of createdUrls) URL.revokeObjectURL(url)
})

async function loadList(): Promise<Attachment[]> {
  if (!props.entityType || !props.entityId) return []
  const res = await listAttachments({ entity_type: props.entityType, entity_id: props.entityId, per_page: 100 })
  const list = res.items ?? []
  await loadThumbs(list)
  return list
}

// 图片附件拉 blob 建 objectURL 作缩略图（附件内容端点要求鉴权，不能直接用 <img src>）；
// 失败回退文件图标。模式与 AttachmentView.loadThumbs 一致。
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

function fmtSize(bytes: number) {
  if (bytes < 1024) return bytes + 'B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + 'KB'
  return (bytes / 1048576).toFixed(1) + 'MB'
}
</script>

<style scoped>
.attachment-list {
  display: grid;
  gap: 8px;
}

.att-item {
  align-items: center;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  display: flex;
  gap: 10px;
  padding: 8px 12px;
}

.att-thumb {
  border-radius: var(--radius-sm);
  flex-shrink: 0;
  height: 48px;
  width: 48px;
}

.att-icon {
  color: var(--text-3);
  flex-shrink: 0;
}

.att-info {
  min-width: 0;
}

.att-name {
  color: var(--text-1);
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.att-meta,
.att-desc {
  color: var(--text-3);
  font-size: 12px;
}

.att-item > .el-button {
  flex-shrink: 0;
  margin-left: auto;
}
</style>
