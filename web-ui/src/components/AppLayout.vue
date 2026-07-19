<template>
  <div class="layout">
    <aside v-if="!isMobile" class="nav">
      <div class="brand">
        <span class="brand-mark">H</span>
        <span class="brand-name">HIAF Lab</span>
      </div>
      <RouterLink v-for="item in navItems" :key="item.path" :to="item.path" class="nav-link">
        <el-icon><component :is="item.icon" /></el-icon>
        <span>{{ item.label }}</span>
      </RouterLink>
    </aside>

    <main class="content">
      <RouterView v-slot="{ Component }">
        <transition name="fade-slide" mode="out-in">
          <component :is="Component" />
        </transition>
      </RouterView>
    </main>

    <nav v-if="isMobile" class="bottom-nav">
      <RouterLink v-for="item in mobileItems" :key="item.path" :to="item.path" class="bottom-link">
        <el-icon><component :is="item.icon" /></el-icon>
        <span>{{ item.label }}</span>
      </RouterLink>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { DataBoard, Document, FolderOpened, MagicStick, Memo, Setting, Tickets, User } from '@element-plus/icons-vue'
import { useMobile } from '../composables/useMobile'
import { useAuthStore } from '../stores/auth'
import { useProjectStore } from '../stores/project'

const isMobile = useMobile()
const auth = useAuthStore()
const projects = useProjectStore()

onMounted(() => {
  projects.load().catch(() => undefined)
})

const navItems = computed(() => {
  const projectId = projects.current?.id || projects.currentId
  const items = [
    { label: '项目', path: '/projects', icon: FolderOpened },
    { label: '日报', path: '/daily-report', icon: Document },
    { label: '问题', path: projectId ? `/projects/${projectId}/issues` : '/issues', icon: Tickets },
    { label: '经验', path: '/experiences', icon: Memo },
    { label: '历史', path: '/daily-reports', icon: DataBoard },
    { label: '审计', path: '/audit', icon: DataBoard },
    { label: '设置', path: '/settings', icon: Setting }
  ]
  if (auth.canReviewAgent) items.push({ label: 'AI审核', path: '/agent-candidates', icon: MagicStick })
  if (auth.isAdmin) items.push({ label: '用户', path: '/admin/users', icon: User })
  return items
})

const mobileItems = computed(() => navItems.value.filter((item) => ['项目', '日报', '问题', '经验', '设置'].includes(item.label)))
</script>

<style scoped>
.layout {
  min-height: 100vh;
}

.nav {
  background: linear-gradient(180deg, var(--navy-800) 0%, var(--navy-900) 100%);
  box-shadow: inset -1px 0 0 rgba(255, 255, 255, 0.04);
  color: #f8fbff;
  display: grid;
  gap: 4px;
  grid-auto-rows: max-content;
  height: 100vh;
  left: 0;
  padding: 20px 12px;
  position: fixed;
  top: 0;
  width: 216px;
}

.brand {
  align-items: center;
  display: flex;
  gap: 10px;
  padding: 4px 10px 22px;
}

.brand-mark {
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 9px;
  box-shadow: 0 4px 12px rgba(20, 112, 138, 0.45);
  color: #fff;
  display: grid;
  font-size: 15px;
  font-weight: 800;
  height: 30px;
  place-items: center;
  width: 30px;
}

.brand-name {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.nav-link,
.bottom-link {
  align-items: center;
  display: flex;
  gap: 8px;
}

.nav-link {
  border-radius: 10px;
  color: #9db1c4;
  font-weight: 500;
  padding: 10px 12px;
  transition:
    background 0.15s ease,
    color 0.15s ease;
}

.nav-link .el-icon {
  font-size: 17px;
}

.nav-link:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #e6eef6;
}

.nav-link.router-link-active {
  background: linear-gradient(135deg, var(--brand-600), var(--brand-500));
  box-shadow: 0 6px 16px -6px rgba(20, 112, 138, 0.55);
  color: #fff;
}

.content {
  margin-left: 216px;
  padding: 24px;
}

.bottom-nav {
  backdrop-filter: blur(14px);
  background: rgba(255, 255, 255, 0.88);
  border-top: 1px solid var(--border);
  bottom: 0;
  box-shadow: 0 -8px 24px -12px rgba(18, 38, 58, 0.18);
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  left: 0;
  position: fixed;
  right: 0;
  z-index: 10;
}

.bottom-link {
  color: var(--text-3);
  flex-direction: column;
  font-size: 12px;
  gap: 4px;
  justify-content: center;
  min-height: 58px;
  transition: color 0.15s ease;
}

.bottom-link.router-link-active {
  color: var(--brand-600);
  font-weight: 600;
}

@media (max-width: 768px) {
  .content {
    margin-left: 0;
    padding: 12px 12px 76px;
  }
}
</style>
