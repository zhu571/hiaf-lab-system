<template>
  <div class="layout" :class="{ 'nav-collapsed': collapsed && !isMobile }">
    <MobileTopBar v-if="isMobile" @ask="openAskDialog" />
    <aside v-if="!isMobile" class="nav">
      <div class="brand">
        <span class="brand-mark">H</span>
        <span class="brand-name">HIAF Lab</span>
      </div>
      <el-tooltip v-for="item in navItems" :key="item.path" :content="item.label" placement="right" :disabled="!collapsed">
        <RouterLink :to="item.path" :class="['nav-link', { 'router-link-active': navActive(item.path) }]">
          <el-badge
            v-if="item.badge === 'agentPending'"
            :value="agentPending"
            :max="99"
            :is-dot="collapsed"
            :hidden="agentPending === 0"
            :title="t('nav.pendingReview')"
            class="nav-badge"
          >
            <el-icon><component :is="item.icon" /></el-icon>
            <span class="nav-label">{{ item.label }}</span>
          </el-badge>
          <template v-else>
            <el-icon><component :is="item.icon" /></el-icon>
            <span class="nav-label">{{ item.label }}</span>
          </template>
        </RouterLink>
      </el-tooltip>
      <el-tooltip :content="t('nav.aiAsk')" placement="right" :disabled="!collapsed">
        <button type="button" class="nav-link nav-ask" @click="openAskDialog">
          <el-icon><ChatDotRound /></el-icon>
          <span class="nav-label">{{ t('nav.aiAsk') }}</span>
        </button>
      </el-tooltip>
      <p class="nav-group">{{ t('nav.systemGroup') }}</p>
      <el-tooltip v-for="item in systemItems" :key="item.path" :content="item.label" placement="right" :disabled="!collapsed">
        <RouterLink :to="item.path" :class="['nav-link', { 'router-link-active': navActive(item.path) }]">
          <el-icon><component :is="item.icon" /></el-icon>
          <span class="nav-label">{{ item.label }}</span>
        </RouterLink>
      </el-tooltip>
    </aside>

    <header v-if="!isMobile" class="topbar">
      <button
        class="collapse-btn"
        type="button"
        :aria-label="collapsed ? t('common.expandSidebar') : t('common.collapseSidebar')"
        @click="collapsed = !collapsed"
      >
        <el-icon><Expand v-if="collapsed" /><Fold v-else /></el-icon>
      </button>
      <AppBreadcrumb :items="breadcrumbItems" class="topbar-breadcrumb" />
      <!-- R2 落位（方案 §2.1 结构图）：命令面板触发框 + 通知中心铃铛，位于用户菜单左侧 -->
      <button
        class="palette-trigger"
        type="button"
        :aria-label="t('palette.placeholder')"
        @click="openPalette"
      >
        <el-icon><Search /></el-icon>
        <span class="palette-trigger-label">{{ t('palette.placeholder') }}</span>
        <kbd class="palette-kbd">Ctrl K</kbd>
      </button>
      <NotificationCenter />
      <el-dropdown class="topbar-user" trigger="click" placement="bottom-end" @command="onUserCommand">
        <button class="user-card-btn" type="button" :aria-label="t('common.userMenu')">
          <span class="user-avatar">{{ avatarText }}</span>
          <span class="user-meta">
            <strong>{{ displayName }}</strong>
            <small>{{ auth.user?.role }}</small>
          </span>
          <el-icon class="user-caret"><ArrowDown /></el-icon>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="settings">{{ t('nav.settings') }}</el-dropdown-item>
            <el-dropdown-item command="logout" divided>{{ t('nav.logout') }}</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </header>

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
    <!-- R2 全局挂载命令面板（对齐 AskDialog 先例）；移动端不启用——无 Ctrl+K 与桌面顶栏（方案 §3.1），零降级 -->
    <CommandPalette v-if="!isMobile" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, watch, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useLocalStorage } from '@vueuse/core'
import { ArrowDown, ChatDotRound, Expand, Fold, Search } from '@element-plus/icons-vue'
import { useMobile } from '@/composables/useMobile'
import { usePolling } from '@/composables/usePolling'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import { useAskDialog } from '@/composables/useAskDialog'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { useAgentPending } from '@/composables/useAgentPending'
import { NAV_ITEMS, filterNavByRole } from '@/config/navigation'
import MobileTopBar from '@/layouts/MobileTopBar.vue'
import AskDialog from '@/components/business/AskDialog.vue'
import CommandPalette from '@/components/business/CommandPalette.vue'
import NotificationCenter from '@/components/business/NotificationCenter.vue'
import AppBreadcrumb from '@/components/base/AppBreadcrumb.vue'

type NavItem = { label: string; path: string; icon: Component; badge?: 'agentPending' }
type Crumb = { label: string; to?: string }

const isMobile = useMobile()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const { t } = useI18n()
const { openAskDialog } = useAskDialog()
const { openPalette } = useCommandPalette()

// 侧栏折叠（结构改版 R1 §2.2）：桌面双态 232↔64（--nav-width ↔ --nav-width-collapsed），
// localStorage 持久化（仅布尔值）；移动端不读取——nav-collapsed class 经 !isMobile 门控，
// 顶栏整体 v-if 不渲染，移动断点样式与本状态零交集。
const collapsed = useLocalStorage('lab-nav-collapsed', false)

onMounted(() => {
  projects.load().catch(() => undefined)
  badgePolling.start()
})

// C11 未读徽章：30s 轮询待审核候选数（复用现有分页接口的 total，零新 API）。
// R2 抽取（方案 §3.2）：计数与拉取收敛为 useAgentPending 模块级单例，侧栏 badge 与
// 通知中心待审组共用同一计数；本处仅保留轮询发起与刷新时机（角色到位/进入候选页），行为不变。
const { agentPending, refreshAgentPending } = useAgentPending()
const badgePolling = usePolling(refreshAgentPending, 30000)

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

// 面包屑（结构改版 R1 §2.3 规则表）：数据源 = route.meta.titleKey + 项目名
//（project store 只读缓存，与 ProjectLayout 同法 find，不回写）。
// 单段页纯标题文本；≥2 段父段可点。/login 为 public 裸渲染不经本壳，此处防御性置空。
const breadcrumbItems = computed<Crumb[]>(() => {
  if (route.meta.public) return []
  const path = route.path
  // 项目工作区：[项目 / 项目名] 两段即止——tab 层级归 ProjectLayout tabs 表达，面包屑不重复
  if (path.startsWith('/projects/')) {
    const id = String(route.params.id || '')
    const name = projects.projects.find((p) => p.id === id)?.name
    if (!name) return [{ label: t('nav.projects') }]
    return [
      { label: t('nav.projects'), to: '/projects' },
      { label: name, to: `/projects/${id}` }
    ]
  }
  // 日报历史：[日报 / 日报历史]（sibling 页定位）
  if (path === '/daily-report/history') {
    return [
      { label: t('nav.dailyReport'), to: '/daily-report' },
      { label: t('dailyHistory.title') }
    ]
  }
  // 日报详情：[日报 / 日报详情]
  if (path.startsWith('/daily-reports/')) {
    return [
      { label: t('nav.dailyReport'), to: '/daily-report/history' },
      { label: t('mobile.title.dailyReportDetail') }
    ]
  }
  const key = route.meta.titleKey as string | undefined
  return key ? [{ label: t(key) }] : []
})

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
  background: linear-gradient(180deg, var(--nav-bg-start) 0%, var(--nav-bg-end) 100%);
  box-shadow: inset -1px 0 0 var(--nav-border);
  color: var(--nav-text-title);
  display: flex;
  flex-direction: column;
  gap: 4px;
  height: 100vh;
  left: 0;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 20px 12px;
  position: fixed;
  top: 0;
  /* 折叠双态（R1）：宽度与顶栏 left / 内容区 margin-left 同帧过渡，不脱节 */
  transition: width var(--dur-base) var(--ease-standard);
  width: var(--nav-width);
}

.layout.nav-collapsed .nav {
  width: var(--nav-width-collapsed);
}

/* 200% 缩放/矮屏下侧栏内容超高时滚动触达（flex 子项 shrink 会先压缩而非溢出，须置 0） */
.nav > * {
  flex-shrink: 0;
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
  box-shadow: var(--nav-mark-shadow);
  color: var(--text-inverse);
  display: grid;
  flex-shrink: 0;
  font-size: 15px;
  font-weight: var(--fw-bold);
  height: 30px;
  place-items: center;
  width: 30px;
}

.brand-name {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
}

/* 折叠态：品牌区紧凑化（只留 H 标居中） */
.layout.nav-collapsed .brand {
  justify-content: center;
  padding-left: 0;
  padding-right: 0;
}

.layout.nav-collapsed .brand-name {
  display: none;
}

.nav-group {
  color: var(--nav-text-group);
  font-size: 12px;
  letter-spacing: 0.08em;
  margin: 14px 10px 2px;
  white-space: nowrap;
}

/* 折叠态：系统组标题隐藏 */
.layout.nav-collapsed .nav-group {
  display: none;
}

.nav-link,
.bottom-link {
  align-items: center;
  display: flex;
  gap: 8px;
}

.nav-link {
  border-radius: var(--radius-md);
  color: var(--nav-text);
  font-weight: 500;
  padding: 10px 12px;
  transition:
    background 0.15s ease,
    color 0.15s ease;
}

.nav-link .el-icon {
  flex-shrink: 0;
  font-size: 17px;
}

.nav-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 折叠态：图标收敛居中、文字隐藏（名称经 el-tooltip 悬浮显示） */
.layout.nav-collapsed .nav-link {
  justify-content: center;
  padding-left: 0;
  padding-right: 0;
}

.layout.nav-collapsed .nav-label {
  display: none;
}

.nav-badge {
  display: inline-flex;
}

/* 展开态：数字角标内联于标签后 */
.nav-badge :deep(.el-badge__content) {
  margin-left: 6px;
  position: static;
  transform: none;
}

/* 折叠态：is-dot 圆点吸附图标右上角（label 已隐藏，badge 内容框 = 图标） */
.layout.nav-collapsed .nav-badge :deep(.el-badge__content) {
  margin-left: 0;
  position: absolute;
  right: -2px;
  top: -2px;
  transform: none;
}

.nav-link:hover {
  background: var(--nav-hover-bg);
  color: var(--nav-text-strong);
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

.layout.nav-collapsed .nav-ask {
  text-align: center;
}

.nav-link.router-link-active {
  background: var(--nav-active-bg);
  box-shadow: var(--nav-active-shadow);
  /* 左光条（视觉改版 S2）：与上行 box-shadow 并列、本声明生效；
     dark 下 --nav-active-accent 覆写为发光值，双主题各自正确（机制见 tokens.css/dark.css 注释） */
  box-shadow: var(--nav-active-accent);
  color: var(--nav-active-color);
}

/* 顶栏（R1 新增）：56px fixed，底 surface + 1px border 分隔 + shadow-sm；z 复用 --z-topbar
   （与 MobileTopBar 同层级但双端互斥渲染，无叠压）；left 双态跟随侧栏宽度同帧过渡 */
.topbar {
  align-items: center;
  background: var(--topbar-bg);
  border-bottom: 1px solid var(--topbar-border);
  box-shadow: var(--shadow-sm);
  display: flex;
  gap: var(--space-4);
  height: var(--topbar-height);
  left: var(--nav-width);
  padding: 0 var(--space-5);
  position: fixed;
  right: 0;
  top: 0;
  transition: left var(--dur-base) var(--ease-standard);
  z-index: var(--z-topbar);
}

.layout.nav-collapsed .topbar {
  left: var(--nav-width-collapsed);
}

.collapse-btn {
  align-items: center;
  background: none;
  border: none;
  border-radius: var(--radius-sm);
  color: var(--text-2);
  cursor: pointer;
  display: inline-flex;
  flex: 0 0 auto;
  font-size: 18px;
  height: 36px;
  justify-content: center;
  padding: 0;
  transition:
    background 0.15s ease,
    color 0.15s ease;
  width: 36px;
}

.collapse-btn:hover {
  background: var(--surface-2);
  color: var(--text-1);
}

.topbar-breadcrumb {
  flex: 1 1 auto;
  min-width: 0;
}

/* 命令面板触发框（R2）：仿输入框样式 + Ctrl K kbd 提示，点击等价于 Ctrl/⌘+K */
.palette-trigger {
  align-items: center;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--text-3);
  cursor: pointer;
  display: inline-flex;
  flex: 0 0 auto;
  font-family: inherit;
  font-size: 13px;
  gap: 8px;
  height: 34px;
  padding: 0 10px;
  transition:
    border-color 0.15s ease,
    color 0.15s ease;
}

.palette-trigger:hover {
  border-color: var(--brand-500);
  color: var(--text-2);
}

.palette-trigger-label {
  white-space: nowrap;
}

.palette-kbd {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-family: inherit;
  font-size: 11px;
  padding: 1px 6px;
}

/* 用户菜单（R1 由侧栏底部迁入顶栏右侧，业界惯例位置）：
   触发按钮保留 user-card-btn 类名（e2e helpers.ts 登出选择器依赖），样式按顶栏语境重写 */
.topbar-user {
  flex: 0 0 auto;
  margin-left: auto;
}

.user-card-btn {
  align-items: center;
  background: none;
  border: none;
  border-radius: var(--radius-md);
  cursor: pointer;
  display: flex;
  font-family: inherit;
  gap: 10px;
  padding: 4px 8px;
  transition: background 0.15s ease;
}

.user-card-btn:hover {
  background: var(--surface-2);
}

.user-avatar {
  align-items: center;
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 8px;
  color: var(--text-inverse);
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
  color: var(--text-1);
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-meta small {
  color: var(--text-3);
  font-size: 11px;
}

.user-caret {
  color: var(--text-3);
}

/* 窄桌面（近 768px 断点）：顶栏横向空间有限，触发框收敛为图标按钮、用户菜单收敛为头像按钮 */
@media (max-width: 1100px) {
  .user-meta,
  .user-caret,
  .palette-trigger-label,
  .palette-kbd {
    display: none;
  }
}

.content {
  margin-left: var(--nav-width);
  /* 顶栏占位：padding-top = 顶栏高 + 原页边距 */
  padding: calc(var(--topbar-height) + var(--space-6)) var(--space-6) var(--space-6);
  transition: margin-left var(--dur-base) var(--ease-standard);
}

.layout.nav-collapsed .content {
  margin-left: var(--nav-width-collapsed);
}

.bottom-nav {
  backdrop-filter: blur(14px);
  background: var(--nav-bottom-bg);
  border-top: 1px solid var(--border);
  bottom: 0;
  box-shadow: var(--nav-bottom-shadow);
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
