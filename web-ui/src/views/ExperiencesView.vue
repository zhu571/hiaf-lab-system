<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('experiences.pageTitle') }}</h2>
      <el-select v-model="selectedProjectId" class="project-select" :placeholder="t('experiences.selectProject')">
        <el-option v-for="p in projects.projects" :key="p.id" :label="p.short_name || p.name" :value="p.id" />
      </el-select>
      <el-button type="primary" @click="dialog = true">{{ t('experiences.create') }}</el-button>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-input v-model="keyword" :placeholder="t('experiences.keyword')" clearable @change="onFilter" />
        <el-input v-model="tagText" :placeholder="t('experiences.tagPlaceholder')" clearable @change="onFilter" />
      </div>
    </section>
    <KanbanBoard :columns="boardColumns">
      <template #card="{ item }">
        <article class="exp-card" @click="open(item)">
          <strong>{{ item.title }}</strong>
          <span class="tags">
            <el-tag v-for="tag in item.tags" :key="tag" size="small" @click.stop="appendTag(tag)">{{ tag }}</el-tag>
          </span>
        </article>
      </template>
      <template #empty>{{ t('experiences.empty') }}</template>
    </KanbanBoard>
    <el-pagination
      v-model:current-page="page"
      v-model:page-size="perPage"
      class="pager"
      layout="total, sizes, prev, pager, next"
      :page-sizes="[20, 50, 100]"
      :total="total"
      @current-change="load"
      @size-change="onFilter"
    />
    <el-drawer v-model="drawer" size="460" :title="t('experiences.detail')">
      <div v-if="selected" class="grid">
        <StatusBadge :value="selected.status" />
        <h3>{{ selected.title }}</h3>
        <MarkdownView :source="selected.content" />
        <div class="tags"><el-tag v-for="tag in selected.tags" :key="tag">{{ tag }}</el-tag></div>
        <el-button v-if="selected.status === 'candidate'" type="primary" @click="publish(selected.id)">{{ t('experiences.publish') }}</el-button>
        <el-button v-if="selected.status === 'published'" @click="archive(selected.id)">{{ t('experiences.archive') }}</el-button>
      </div>
    </el-drawer>
    <FormDialog v-model="dialog" :title="t('experiences.create')" width="620" @submit="create">
      <el-form-item :label="t('experiences.labelTitle')"><el-input v-model="draft.title" /></el-form-item>
      <el-form-item :label="t('experiences.labelTags')"><el-input v-model="draft.tags" /></el-form-item>
      <el-form-item :label="t('experiences.labelContent')"><el-input v-model="draft.content" type="textarea" :rows="6" /></el-form-item>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '@/components/base/StatusBadge.vue'
import KanbanBoard from '@/components/base/KanbanBoard.vue'
import FormDialog from '@/components/base/FormDialog.vue'
import MarkdownView from '@/components/business/MarkdownView.vue'
import { archiveExperience, createExperience, listExperiences, publishExperience, type Experience } from '../api/experiences'
import { useProjectStore } from '../stores/project'

const { t } = useI18n()
const projects = useProjectStore()
const items = ref<Experience[]>([])
const selected = ref<Experience | null>(null)
const drawer = ref(false)
const dialog = ref(false)
const keyword = ref('')
const tagText = ref('')
const page = ref(1)
const perPage = ref(20)
const total = ref(0)
const draft = reactive({ title: '', content: '', tags: '' })
const columns = [
  { status: 'candidate', labelKey: 'experiences.columnCandidate', tone: '--warn' },
  { status: 'published', labelKey: 'experiences.columnPublished', tone: '--ok' },
  { status: 'archived', labelKey: 'experiences.columnArchived', tone: '--info' }
]

const projectId = computed(() => projects.current?.id || '')
const selectedProjectId = computed({
  get: () => projectId.value,
  set: (id: string) => {
    if (id) projects.select(id)
  }
})
const grouped = computed(
  () => Object.fromEntries(columns.map((col) => [col.status, items.value.filter((item) => item.status === col.status)])) as Record<string, Experience[]>
)

// 看板列数据（R5 KanbanBoard 接入）：tone 等值原 [data-status] 圆点色规则（candidate --warn / published --ok / archived --info）
const boardColumns = computed(() =>
  columns.map((col) => ({ key: col.status, label: t(col.labelKey), tone: col.tone, items: grouped.value[col.status] }))
)

onMounted(load)
watch(projectId, () => {
  page.value = 1
  load()
})

function onFilter() {
  page.value = 1
  load()
}

async function load() {
  try {
    await projects.load()
    const results = await Promise.all(
      columns.map((col) =>
        listExperiences({ status: col.status, keyword: keyword.value, tags: tagText.value, project_id: projectId.value, page: page.value, per_page: perPage.value })
      )
    )
    items.value = results.flatMap((result) => result.items ?? [])
    // 三个状态列各自独立分页，总数为三列之和
    total.value = results.reduce((sum, result) => sum + (result.total ?? 0), 0)
  } catch (err) {
    showApiError(err, t('experiences.loadFailed'))
  }
}

function appendTag(tag: string) {
  const tags = new Set(tagText.value.split(',').map((item) => item.trim()).filter(Boolean))
  tags.add(tag)
  tagText.value = Array.from(tags).join(',')
  onFilter()
}

function open(item: Experience) {
  selected.value = item
  drawer.value = true
}

async function publish(id: string) {
  try {
    selected.value = await publishExperience(id)
    ElMessage.success(t('experiences.published'))
    await load()
  } catch (err) {
    showApiError(err, t('experiences.publishFailed'))
  }
}

async function archive(id: string) {
  try {
    selected.value = await archiveExperience(id)
    ElMessage.success(t('experiences.archived'))
    await load()
  } catch (err) {
    showApiError(err, t('experiences.archiveFailed'))
  }
}

async function create() {
  try {
    await createExperience({ project_id: projectId.value || undefined, title: draft.title, content: draft.content, tags: draft.tags.split(',').map((item) => item.trim()).filter(Boolean) })
    dialog.value = false
    ElMessage.success(t('experiences.saved'))
    await load()
  } catch (err) {
    showApiError(err, t('experiences.saveFailed'))
  }
}
</script>

<style scoped>
.project-select {
  max-width: 240px;
}

.filters-panel {
  padding: 14px 20px;
}

.filters,
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.filters .el-input {
  max-width: 240px;
}

/* 看板骨架（.board/.column/.column-head/.dot/[data-status] 圆点色/.count/.empty-hint）已收口
   base/KanbanBoard（R5）；列圆点色改由列定义 tone 传入 */

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.exp-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  transition: var(--dur-base) var(--ease-standard);
}

.exp-card:hover {
  border-color: var(--brand-400);
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.exp-card strong {
  color: var(--text-1);
  font-size: 14px;
  line-height: 1.4;
}

.tags .el-tag {
  cursor: pointer;
}

@media (max-width: 768px) {
  .filters .el-input {
    max-width: none;
    width: 100%;
  }
}
</style>
