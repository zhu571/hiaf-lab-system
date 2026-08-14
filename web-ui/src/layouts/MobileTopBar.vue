<template>
  <header class="mobile-topbar">
    <button v-if="showBack" class="back-btn" type="button" :aria-label="t('mobile.back')" @click="goBack">
      <el-icon><ArrowLeft /></el-icon>
    </button>
    <h1 class="title">{{ title }}</h1>
    <button class="ask-btn" type="button" :aria-label="t('ask.title')" @click="emit('ask')">
      <el-icon><ChatDotRound /></el-icon>
    </button>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ChatDotRound } from '@element-plus/icons-vue'
import { useProjectStore } from '@/stores/project'

const emit = defineEmits<{ ask: [] }>()

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const projects = useProjectStore()

// 底栏 5 项对应一级路径与 /login 隐藏返回键，其余路径显示
const NO_BACK_PATHS = ['/', '/projects', '/todos', '/daily-report', '/settings', '/login']

const isProjectWorkspace = computed(() => route.path.startsWith('/projects/') && route.path !== '/projects')

const showBack = computed(() => !NO_BACK_PATHS.includes(route.path))

const title = computed(() => {
  if (isProjectWorkspace.value && projects.current?.name) return projects.current.name
  const key = route.meta.titleKey as string | undefined
  return key ? t(key) : ''
})

function goBack() {
  // 直接打开深链时 history.state.back 为空，回退到首页
  if (window.history.state?.back == null) {
    router.push('/')
    return
  }
  router.back()
}
</script>

<style scoped>
.mobile-topbar {
  align-items: center;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  box-shadow: var(--shadow-sm);
  display: flex;
  gap: 4px;
  height: calc(var(--mobile-topbar-height) + var(--safe-area-top));
  left: 0;
  padding-top: var(--safe-area-top);
  position: fixed;
  right: 0;
  top: 0;
  z-index: 10;
}

.back-btn {
  align-items: center;
  background: transparent;
  border: none;
  border-radius: var(--radius-sm);
  color: var(--text-1);
  cursor: pointer;
  display: inline-flex;
  flex: 0 0 auto;
  font-size: 18px;
  height: 48px;
  justify-content: center;
  padding: 0 12px;
  width: 48px;
}

.ask-btn {
  align-items: center;
  background: transparent;
  border: none;
  border-radius: var(--radius-sm);
  color: var(--brand-600);
  cursor: pointer;
  display: inline-flex;
  flex: 0 0 auto;
  font-size: 18px;
  height: 48px;
  justify-content: center;
  margin-left: auto;
  padding: 0 12px;
  width: 48px;
}

.back-btn:active,
.ask-btn:active {
  background: var(--surface-2);
}

.title {
  color: var(--text-1);
  font-size: 17px;
  font-weight: 650;
  letter-spacing: -0.01em;
  margin: 0;
  min-width: 0;
  overflow: hidden;
  padding-right: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
