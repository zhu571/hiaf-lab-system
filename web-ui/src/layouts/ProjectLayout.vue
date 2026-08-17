<template>
  <div class="page">
    <section v-if="!ready" v-loading="true" class="panel workspace-loading" />
    <div v-else-if="!project" class="fallback-page">
      <el-empty :description="fallbackText" />
      <el-button type="primary" @click="router.push('/projects')">{{ t('project.goToProjects') }}</el-button>
    </div>
    <template v-else>
      <div class="workspace-sticky">
        <section class="panel workspace-head">
          <el-tooltip :content="t('project.backToList')" placement="top">
            <el-button class="back-btn" circle :aria-label="t('project.backToList')" @click="router.push('/projects')">
              <el-icon><ArrowLeft /></el-icon>
            </el-button>
          </el-tooltip>
          <div class="title-block">
            <h2>{{ project.name }}</h2>
            <span v-if="project.code" class="code">({{ project.code }})</span>
            <el-tag :type="stage.type" size="small" effect="light">{{ stage.label }}</el-tag>
          </div>
          <el-select :model-value="projectId" class="switch-select" :placeholder="t('project.switchProject')" @change="switchProject">
            <el-option v-for="p in projects.projects" :key="p.id" :label="p.short_name || p.name" :value="p.id" />
          </el-select>
        </section>
        <el-tabs :model-value="activeTab" class="workspace-tabs" @tab-change="onTabChange">
          <el-tab-pane v-for="tab in tabs" :key="tab.name" :label="tab.label" :name="tab.name" />
        </el-tabs>
      </div>
      <RouterView />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useProjectStore } from '@/stores/project'
import { statusMetaFor } from '@/utils/statusMeta'

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()
const { t } = useI18n()
const ready = ref(false)

const tabs = computed(() => [
  { label: t('project.tabs.overview'), name: 'overview', path: '' },
  { label: t('project.tabs.issues'), name: 'issues', path: 'issues' },
  { label: t('project.tabs.runs'), name: 'experiment-runs', path: 'experiment-runs' },
  { label: t('project.tabs.testData'), name: 'test-data', path: 'test-data' },
  { label: t('project.tabs.rfMatching'), name: 'rf-matching', path: 'rf-matching' },
  { label: t('project.tabs.assembly'), name: 'assembly', path: 'assembly' }
])

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

// 阶段标签走 statusMeta projectStage 注册表（美术 §3.8 域覆盖登记点，原本地 STAGE_TYPES 双轨映射已并入；
// 语义族对齐 §3.8 总表：draft=warning、active/completed=success、archived=info）
const stage = computed(() => {
  const status = project.value?.status || ''
  const meta = statusMetaFor('projectStage', status)
  return {
    label: meta ? t(meta.labelKey) : project.value?.status || t('project.stages.unknown'),
    type: meta?.tone || ('info' as const)
  }
})

const fallbackText = computed(() => (projects.projects.length ? t('project.fallbackNoAccess') : t('project.fallbackNoProjects')))

const activeTab = computed(() => String(route.path.split('/')[3] || 'overview'))

function onTabChange(name: string | number) {
  const tab = tabs.value.find((t) => t.name === name)
  if (!tab || !projectId.value) return
  const target = `/projects/${projectId.value}${tab.path ? `/${tab.path}` : ''}`
  if (route.path !== target) router.push(target)
}

function switchProject(id: string) {
  if (!id || id === projectId.value) return
  projects.select(id)
  const tab = tabs.value.find((t) => t.name === activeTab.value)
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

/* ---------- 吸顶工作区头（R6 §7.2） ----------
   head + tabs 由单个 sticky 容器包裹：避免两个独立 sticky 元素同 top 叠压；
   桌面 top 跟随顶栏（--topbar-height），移动端计入 MobileTopBar 实际高度（48px + safe-area）；
   背景 var(--bg) 防内容透出；z-index 低于顶栏（--z-topbar: 10），高于页内 EP 固定列 */

.workspace-sticky {
  background: var(--bg);
  position: sticky;
  top: var(--topbar-height);
  z-index: calc(var(--z-topbar) - 1);
}

.workspace-head {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.back-btn {
  flex-shrink: 0;
}

.title-block {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  min-width: 0;
}

.title-block h2 {
  font-size: var(--fs-title);
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
  .workspace-sticky {
    top: calc(var(--mobile-topbar-height) + var(--safe-area-top));
  }

  .switch-select {
    margin-left: 0;
    max-width: none;
    width: 100%;
  }

  .workspace-tabs :deep(.el-tabs__item) {
    font-size: 13px;
    padding: 0 12px;
  }
}
</style>
