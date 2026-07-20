<template>
  <div class="layout">
    <aside v-if="!isMobile" class="nav">
      <div class="brand">
        <span class="brand-mark">H</span>
        <span class="brand-name">HIAF Lab</span>
      </div>
      <RouterLink
        v-for="item in navItems"
        :key="item.path"
        :to="item.path"
        :class="['nav-link', { 'router-link-active': navActive(item.path) }]"
      >
        <el-icon><component :is="item.icon" /></el-icon>
        <span>{{ item.label }}</span>
      </RouterLink>
      <p class="nav-group">系统</p>
      <RouterLink
        v-for="item in systemItems"
        :key="item.path"
        :to="item.path"
        :class="['nav-link', { 'router-link-active': navActive(item.path) }]"
      >
        <el-icon><component :is="item.icon" /></el-icon>
        <span>{{ item.label }}</span>
      </RouterLink>
      <el-dropdown class="user-card" trigger="click" placement="top-start" @command="onUserCommand">
        <button class="user-card-btn" type="button">
          <span class="user-avatar">{{ avatarText }}</span>
          <span class="user-meta">
            <strong>{{ displayName }}</strong>
            <small>{{ auth.user?.role }}</small>
          </span>
          <el-icon class="user-caret"><ArrowUp /></el-icon>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="settings">个人设置</el-dropdown-item>
            <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </aside>

    <main class="content">
      <RouterView v-slot="{ Component }">
        <transition name="fade-slide" mode="out-in">
          <component :is="Component" />
        </transition>
      </RouterView>
    </main>

    <nav v-if="isMobile" class="bottom-nav">
      <RouterLink
        v-for="item in mobileItems"
        :key="item.path"
        :to="item.path"
        :class="['bottom-link', { 'router-link-active': navActive(item.path) }]"
      >
        <el-icon><component :is="item.icon" /></el-icon>
        <span>{{ item.label }}</span>
      </RouterLink>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowUp, Connection, DataBoard, Document, FolderOpened, MagicStick, Memo, Monitor, Paperclip, Setting, User } from '@element-plus/icons-vue'
import { useMobile } from '../composables/useMobile'
import { useAuthStore } from '../stores/auth'
import { useProjectStore } from '../stores/project'

type NavItem = { label: string; path: string; icon: Component }

const isMobile = useMobile()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()

onMounted(() => {
  projects.load().catch(() => undefined)
})

const navItems = computed<NavItem[]>(() => {
  const items: NavItem[] = [
    { label: '项目', path: '/projects', icon: FolderOpened },
    { label: '日报', path: '/daily-report', icon: Document },
    { label: '经验库', path: '/experiences', icon: Memo },
    { label: '附件', path: '/attachments', icon: Paperclip }
  ]
  if (auth.canReviewAgent) items.push({ label: 'AI审核', path: '/agent-candidates', icon: MagicStick })
  return items
})

const systemItems = computed<NavItem[]>(() => {
  const items: NavItem[] = [
    { label: '仪器', path: '/instruments', icon: Monitor },
    { label: '传感器', path: '/sensors', icon: Connection }
  ]
  if (auth.isAdmin) items.push({ label: '用户管理', path: '/admin/users', icon: User })
  items.push({ label: '审计', path: '/audit', icon: DataBoard })
  return items
})

const mobileItems = computed<NavItem[]>(() => {
  const items: NavItem[] = [
    { label: '项目', path: '/projects', icon: FolderOpened },
    { label: '日报', path: '/daily-report', icon: Document },
    { label: '经验', path: '/experiences', icon: Memo },
    { label: '附件', path: '/attachments', icon: Paperclip },
    { label: '我的', path: '/settings', icon: Setting }
  ]
  return items
})

// RouterLink 的自动高亮只匹配同一条路由记录，/projects/:id/* 与 /projects 是兄弟记录，
// 因此按路径前缀手动判断（其余一级路径也统一走这个规则）
function navActive(path: string) {
  return route.path === path || route.path.startsWith(`${path}/`)
}

const displayName = computed(() => auth.user?.display_name || auth.user?.username || '')
const avatarText = computed(() => displayName.value.slice(0, 1).toUpperCase() || '?')

async function onUserCommand(command: string | number | object) {
  if (command === 'settings') {
    router.push('/settings')
    return
  }
  if (command === 'logout') {
    try {
      await auth.logout()
    } catch {
      // 退出接口失败也强制回登录页，由路由守卫兜底重新鉴权
    }
    router.push('/login')
  }
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
}

.nav {
  background: linear-gradient(180deg, var(--navy-800) 0%, var(--navy-900) 100%);
  box-shadow: inset -1px 0 0 rgba(255, 255, 255, 0.04);
  color: #f8fbff;
  display: flex;
  flex-direction: column;
  gap: 4px;
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

.nav-group {
  color: #64798e;
  font-size: 12px;
  letter-spacing: 0.08em;
  margin: 14px 10px 2px;
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

.user-card {
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  margin-top: auto;
  padding-top: 12px;
}

.user-card-btn {
  align-items: center;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 10px;
  color: #e6eef6;
  cursor: pointer;
  display: flex;
  gap: 10px;
  padding: 8px 10px;
  transition: background 0.15s ease;
  width: 100%;
}

.user-card-btn:hover {
  background: rgba(255, 255, 255, 0.08);
}

.user-avatar {
  align-items: center;
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 8px;
  color: #fff;
  display: inline-flex;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 700;
  height: 30px;
  justify-content: center;
  width: 30px;
}

.user-meta {
  display: grid;
  line-height: 1.3;
  min-width: 0;
  text-align: left;
}

.user-meta strong {
  color: #f8fbff;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-meta small {
  color: #9db1c4;
  font-size: 11px;
}

.user-caret {
  color: #9db1c4;
  margin-left: auto;
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
