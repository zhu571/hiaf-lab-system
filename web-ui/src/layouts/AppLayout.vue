<template>
  <div class="layout">
    <MobileTopBar v-if="isMobile" @ask="openAskDialog" />
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
        <el-badge
          v-if="item.badge === 'agentPending'"
          :value="agentPending"
          :max="99"
          :hidden="agentPending === 0"
          :title="t('nav.pendingReview')"
          class="nav-badge"
        >
          <span>{{ item.label }}</span>
        </el-badge>
        <span v-else>{{ item.label }}</span>
      </RouterLink>
      <button type="button" class="nav-link nav-ask" @click="openAskDialog">
        <el-icon><ChatDotRound /></el-icon>
        <span>{{ t('nav.aiAsk') }}</span>
      </button>
      <p class="nav-group">{{ t('nav.systemGroup') }}</p>
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
            <el-dropdown-item command="settings">{{ t('nav.settings') }}</el-dropdown-item>
            <el-dropdown-item command="logout" divided>{{ t('nav.logout') }}</el-dropdown-item>
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

    <AskDialog />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowUp, ChatDotRound } from '@element-plus/icons-vue'
import { useMobile } from '@/composables/useMobile'
import { usePolling } from '@/composables/usePolling'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import { useAskDialog } from '@/composables/useAskDialog'
import { listAgentCandidates } from '@/api/agent'
import { NAV_ITEMS, filterNavByRole } from '@/config/navigation'
import MobileTopBar from '@/layouts/MobileTopBar.vue'
import AskDialog from '@/components/business/AskDialog.vue'

type NavItem = { label: string; path: string; icon: Component; badge?: 'agentPending' }

const isMobile = useMobile()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const { t } = useI18n()
const { openAskDialog } = useAskDialog()

onMounted(() => {
  projects.load().catch(() => undefined)
  badgePolling.start()
})

// C11 未读徽章：30s 轮询待审核候选数（复用现有分页接口的 total，零新 API）。
// 仅 admin/maintainer（与 ListCandidates 后端权限一致）拉取；页面隐藏时暂停（usePolling
// pauseOnHidden），恢复可见、角色资料到位、进入候选页（审核后计数变化）时立即刷新。
const agentPending = ref(0)
const badgePolling = usePolling(refreshAgentPending, 30000)

async function refreshAgentPending() {
  if (!auth.canReviewAgent || document.hidden) return
  try {
    const data = await listAgentCandidates({ status: 'pending_review', page: 1, per_page: 1 })
    agentPending.value = data.total
  } catch {
    // 徽章拉取失败静默降级，下一轮轮询再试，不打断导航
  }
}

watch(
  () => auth.canReviewAgent,
  (ok) => {
    if (ok) refreshAgentPending()
  },
  { immediate: true }
)
watch(
  () => route.path,
  (p) => {
    if (p.startsWith('/agent-candidates')) refreshAgentPending()
  }
)

// 导航单一数据源（重构方案 §3.3）：三组均从 NAV_ITEMS 过滤派生，角色判断收敛到 filterNavByRole 纯函数。
// 桌面主组/系统组按 group 过滤 + minRole 过滤；移动底栏取 mobile:true 项（settings 为仅移动端项）。
const navItems = computed<NavItem[]>(() =>
  filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'main'), auth.user?.role ?? '').map((i) => ({
    label: t(i.titleKey),
    path: i.path,
    icon: i.icon,
    badge: i.badge
  }))
)

const systemItems = computed<NavItem[]>(() =>
  filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'system' && !i.mobile), auth.user?.role ?? '').map((i) => ({
    label: t(i.titleKey),
    path: i.path,
    icon: i.icon
  }))
)

const mobileItems = computed<NavItem[]>(() =>
  filterNavByRole(NAV_ITEMS.filter((i) => i.mobile), auth.user?.role ?? '').map((i) => ({
    label: t(i.shortTitleKey ?? i.titleKey),
    path: i.path,
    icon: i.icon
  }))
)

// RouterLink 的自动高亮只匹配同一条路由记录，/projects/:id/* 与 /projects 是兄弟记录，
// 因此按路径前缀手动判断（其余一级路径也统一走这个规则）
function navActive(path: string) {
  if (path === '/') return route.path === '/'
  return route.path === path || route.path.startsWith(path + '/')
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

.nav-badge {
  display: inline-flex;
}

.nav-badge :deep(.el-badge__content) {
  margin-left: 6px;
  position: static;
  transform: none;
}

.nav-link:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #e6eef6;
}

.nav-ask {
  background: none;
  border: none;
  cursor: pointer;
  font-family: inherit;
  font-size: inherit;
  text-align: left;
  width: 100%;
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
  padding: var(--space-6);
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
  padding-bottom: var(--safe-area-bottom);
  position: fixed;
  right: 0;
  z-index: var(--z-bottomnav);
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
    padding: calc(var(--mobile-topbar-height) + var(--safe-area-top) + 8px) 12px calc(var(--bottom-nav-height) + var(--safe-area-bottom) + 12px);
  }
}
</style>
