<template>
  <div class="page">
    <div class="toolbar">
      <h2>经验库</h2>
      <el-button type="primary" @click="dialog = true">新增经验</el-button>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-input v-model="keyword" placeholder="关键词" clearable @change="load" />
        <el-input v-model="tagText" placeholder="标签，逗号分隔" clearable @change="load" />
      </div>
    </section>
    <div class="board">
      <section v-for="col in columns" :key="col.status" class="panel column" :data-status="col.status">
        <div class="column-head">
          <h3><span class="dot" />{{ col.label }}</h3>
          <span class="count">{{ grouped[col.status].length }}</span>
        </div>
        <article v-for="item in grouped[col.status]" :key="item.id" class="exp-card" @click="open(item)">
          <strong>{{ item.title }}</strong>
          <span class="tags">
            <el-tag v-for="tag in item.tags" :key="tag" size="small" @click.stop="appendTag(tag)">{{ tag }}</el-tag>
          </span>
        </article>
        <p v-if="grouped[col.status].length === 0" class="empty-hint">暂无经验</p>
      </section>
    </div>
    <el-drawer v-model="drawer" size="460" title="经验详情">
      <div v-if="selected" class="grid">
        <StatusBadge :value="selected.status" />
        <h3>{{ selected.title }}</h3>
        <p class="exp-content">{{ selected.content }}</p>
        <div class="tags"><el-tag v-for="tag in selected.tags" :key="tag">{{ tag }}</el-tag></div>
        <el-button v-if="selected.status === 'candidate'" type="primary" @click="publish(selected.id)">发布</el-button>
        <el-button v-if="selected.status === 'published'" @click="archive(selected.id)">归档</el-button>
      </div>
    </el-drawer>
    <el-dialog v-model="dialog" title="新增经验" width="620">
      <el-form label-position="top">
        <el-form-item label="标题"><el-input v-model="draft.title" /></el-form-item>
        <el-form-item label="标签"><el-input v-model="draft.tags" /></el-form-item>
        <el-form-item label="内容"><el-input v-model="draft.content" type="textarea" :rows="6" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" @click="create">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { archiveExperience, createExperience, listExperiences, publishExperience, type Experience } from '../api/experiences'

const items = ref<Experience[]>([])
const selected = ref<Experience | null>(null)
const drawer = ref(false)
const dialog = ref(false)
const keyword = ref('')
const tagText = ref('')
const draft = reactive({ title: '', content: '', tags: '' })
const columns = [
  { status: 'candidate', label: '待审核' },
  { status: 'published', label: '已发布' },
  { status: 'archived', label: '已归档' }
]

const grouped = computed(
  () => Object.fromEntries(columns.map((col) => [col.status, items.value.filter((item) => item.status === col.status)])) as Record<string, Experience[]>
)

onMounted(load)

async function load() {
  try {
    const results = await Promise.all(
      columns.map((col) => listExperiences({ status: col.status, keyword: keyword.value, tags: tagText.value, per_page: 100 }))
    )
    items.value = results.flatMap((result) => result.items)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '经验加载失败')
  }
}

function appendTag(tag: string) {
  const tags = new Set(tagText.value.split(',').map((item) => item.trim()).filter(Boolean))
  tags.add(tag)
  tagText.value = Array.from(tags).join(',')
  load()
}

function open(item: Experience) {
  selected.value = item
  drawer.value = true
}

async function publish(id: string) {
  try {
    selected.value = await publishExperience(id)
    ElMessage.success('经验已发布')
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '发布失败')
  }
}

async function archive(id: string) {
  try {
    selected.value = await archiveExperience(id)
    ElMessage.success('经验已归档')
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '归档失败')
  }
}

async function create() {
  try {
    await createExperience({ title: draft.title, content: draft.content, tags: draft.tags.split(',').map((item) => item.trim()).filter(Boolean) })
    dialog.value = false
    ElMessage.success('经验已保存')
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存失败')
  }
}
</script>

<style scoped>
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

.board {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.column {
  align-content: start;
  background: var(--surface-2);
  display: grid;
  gap: 12px;
}

.column-head {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.column-head h3 {
  align-items: center;
  display: flex;
  font-size: 14px;
  gap: 8px;
  letter-spacing: 0.01em;
}

.dot {
  background: var(--text-3);
  border-radius: 50%;
  height: 8px;
  width: 8px;
}

[data-status='candidate'] .dot {
  background: var(--warn);
}

[data-status='published'] .dot {
  background: var(--ok);
}

[data-status='archived'] .dot {
  background: #9099a5;
}

.count {
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--text-3);
  font-size: 12px;
  font-weight: 600;
  min-width: 26px;
  padding: 0 8px;
  text-align: center;
}

.exp-card {
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    transform 0.15s ease;
}

.exp-card:hover {
  border-color: var(--brand-100);
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

.empty-hint {
  border: 1px dashed var(--border-strong);
  border-radius: 10px;
  color: var(--text-3);
  font-size: 12px;
  padding: 16px 0;
  text-align: center;
}

.exp-content {
  color: var(--text-2);
  white-space: pre-wrap;
}

@media (max-width: 768px) {
  .board {
    grid-template-columns: 1fr;
  }

  .filters .el-input {
    max-width: none;
    width: 100%;
  }
}
</style>
