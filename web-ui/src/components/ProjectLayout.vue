<template>
  <div class="page">
    <section v-if="!ready" v-loading="true" class="panel workspace-loading" />
    <div v-else-if="!project" class="fallback-page">
      <el-empty :description="fallbackText" />
      <el-button type="primary" @click="router.push('/projects')">前往项目列表</el-button>
    </div>
    <template v-else>
      <section class="panel workspace-head">
        <el-button class="back-btn" @click="router.push('/projects')">
          <el-icon><ArrowLeft /></el-icon>
          项目列表
        </el-button>
        <div class="title-block">
          <h2>{{ project.name }}</h2>
          <span v-if="project.code" class="code">({{ project.code }})</span>
          <el-tag :type="stage.type" size="small" effect="light">{{ stage.label }}</el-tag>
        </div>
        <el-select :model-value="projectId" class="switch-select" placeholder="切换项目" @change="switchProject">
          <el-option v-for="p in projects.projects" :key="p.id" :label="p.short_name || p.name" :value="p.id" />
        </el-select>
      </section>
      <el-tabs :model-value="activeTab" class="workspace-tabs" @tab-change="onTabChange">
        <el-tab-pane v-for="tab in tabs" :key="tab.name" :label="tab.label" :name="tab.name" />
      </el-tabs>
      <RouterView />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useProjectStore } from '../stores/project'

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()
const ready = ref(false)

const tabs = [
  { label: '概览', name: 'overview', path: '' },
  { label: '问题', name: 'issues', path: 'issues' },
  { label: '批次', name: 'experiment-runs', path: 'experiment-runs' },
  { label: '数据', name: 'test-data', path: 'test-data' },
  { label: 'RF匹配', name: 'rf-matching', path: 'rf-matching' },
  { label: '装配', name: 'assembly', path: 'assembly' }
]

// 项目上下文的唯一事实来源是路由参数；store 只作为跨页共享缓存跟随同步
const projectId = computed(() => String(route.params.id || ''))
const project = computed(() => projects.projects.find((p) => p.id === projectId.value))

onMounted(async () => {
  try {
    await projects.load()
  } catch {
    // 列表加载失败按无项目处理，由引导页收口
  } finally {
    ready.value = true
  }
})

watch(
  project,
  (p) => {
    if (p && projects.currentId !== p.id) projects.select(p.id)
  },
  { immediate: true }
)

const STAGE_META: Record<string, { label: string; type: 'primary' | 'success' | 'info' | 'warning' }> = {
  draft: { label: '筹备', type: 'info' },
  active: { label: '进行中', type: 'success' },
  completed: { label: '已完成', type: 'primary' },
  archived: { label: '归档', type: 'info' }
}
const stage = computed(() => STAGE_META[project.value?.status || ''] || { label: project.value?.status || '未知', type: 'info' as const })

const fallbackText = computed(() => (projects.projects.length ? '项目不存在或无权访问，请重新选择' : '暂无项目，请先创建或选择一个项目'))

const activeTab = computed(() => String(route.path.split('/')[3] || 'overview'))

function onTabChange(name: string | number) {
  const tab = tabs.find((t) => t.name === name)
  if (!tab || !projectId.value) return
  const target = `/projects/${projectId.value}${tab.path ? `/${tab.path}` : ''}`
  if (route.path !== target) router.push(target)
}

function switchProject(id: string) {
  if (!id || id === projectId.value) return
  projects.select(id)
  const tab = tabs.find((t) => t.name === activeTab.value)
  router.push(`/projects/${id}${tab?.path ? `/${tab.path}` : ''}`)
}
</script>

<style scoped>
.workspace-loading {
  min-height: 240px;
}

.fallback-page {
  align-items: center;
  display: flex;
  flex-direction: column;
  gap: 16px;
  justify-content: center;
  min-height: 400px;
}

.workspace-head {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.title-block {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  min-width: 0;
}

.title-block h2 {
  font-size: 18px;
}

.code {
  color: var(--text-3);
  font-size: 13px;
}

.switch-select {
  margin-left: auto;
  max-width: 220px;
}

.workspace-tabs :deep(.el-tabs__header) {
  margin-bottom: 0;
}

@media (max-width: 768px) {
  .switch-select {
    margin-left: 0;
    max-width: none;
    width: 100%;
  }
}
</style>
