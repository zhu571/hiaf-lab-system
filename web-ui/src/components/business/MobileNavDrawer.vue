<template>
  <el-drawer v-model="open" :title="t('mobile.drawer.title')" size="78%" class="mobile-nav-drawer">
    <nav :aria-label="t('mobile.drawer.title')">
      <section v-for="group in groups" :key="group.name" class="drawer-group">
        <h3>{{ group.label }}</h3>
        <RouterLink
          v-for="item in group.items"
          :key="item.path"
          :to="item.path"
          class="drawer-link"
          @click="open = false"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ t(item.titleKey) }}</span>
        </RouterLink>
      </section>
    </nav>

    <section class="drawer-user">
      <span class="drawer-avatar">{{ avatarText }}</span>
      <div>
        <strong>{{ displayName }}</strong>
        <small>{{ auth.user?.role }}</small>
      </div>
      <NotificationCenter />
    </section>
    <div class="drawer-actions">
      <el-button @click="goSettings">{{ t('nav.settings') }}</el-button>
      <el-button @click="logout">{{ t('nav.logout') }}</el-button>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NAV_ITEMS, filterNavByRole, groupNavBySection, type NavSection } from '@/config/navigation'
import { useAuthStore } from '@/stores/auth'
import NotificationCenter from '@/components/business/NotificationCenter.vue'

const open = defineModel<boolean>({ required: true })
const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()

const SECTION_TITLE_KEYS: Record<NavSection, string> = {
  device: 'nav.sections.device',
  manage: 'nav.sections.manage'
}

// 分组与桌面侧栏同源（nav-menu-redesign 方案 §3.3）：main 组 + system 组按 section 聚类，
// settings（仅移动端项）随 manage 组出现在抽屉末尾。
const groups = computed(() => {
  const role = auth.user?.role ?? ''
  const main = {
    name: 'main',
    label: t('mobile.drawer.main'),
    items: filterNavByRole(NAV_ITEMS.filter((item) => item.group === 'main'), role)
  }
  const sections = groupNavBySection(
    filterNavByRole(NAV_ITEMS.filter((item) => item.group === 'system'), role)
  ).map((g) => ({
    name: g.section,
    label: t(SECTION_TITLE_KEYS[g.section]),
    items: g.items
  }))
  return [main, ...sections]
})

const displayName = computed(() => auth.user?.display_name || auth.user?.username || '')
const avatarText = computed(() => displayName.value.slice(0, 1).toUpperCase() || '?')

function goSettings() {
  open.value = false
  router.push('/settings')
}

async function logout() {
  open.value = false
  try {
    await auth.logout()
  } catch {
    // 后端登出失败也强制回登录页，由路由守卫重新鉴权
  }
  router.push('/login')
}
</script>

<style scoped>
.drawer-group {
  display: grid;
  gap: var(--space-1);
  margin-bottom: var(--space-5);
}

.drawer-group h3 {
  color: var(--text-3);
  font-size: 12px;
  letter-spacing: 0.08em;
  margin: 0 0 var(--space-1);
  text-transform: uppercase;
}

.drawer-link {
  align-items: center;
  border-radius: var(--radius-md);
  color: var(--text-2);
  display: flex;
  gap: var(--space-3);
  min-height: 42px;
  padding: 0 var(--space-3);
}

.drawer-link.router-link-active,
.drawer-link:active {
  background: var(--brand-050);
  color: var(--brand-600);
}

.drawer-user {
  align-items: center;
  border-top: 1px solid var(--border);
  display: flex;
  gap: var(--space-3);
  padding-top: var(--space-4);
}

.drawer-user > div {
  display: grid;
  flex: 1;
}

.drawer-user small {
  color: var(--text-3);
}

.drawer-avatar {
  align-items: center;
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: var(--radius-md);
  color: var(--text-inverse);
  display: inline-flex;
  font-weight: var(--fw-bold);
  height: 36px;
  justify-content: center;
  width: 36px;
}

.drawer-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-4);
}
</style>
