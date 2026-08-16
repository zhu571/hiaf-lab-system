<template>
  <el-dialog
    v-model="paletteOpen"
    :show-close="false"
    top="12vh"
    width="640px"
    class="command-palette"
    @opened="onOpened"
  >
    <div class="palette-box">
      <div class="palette-input-row">
        <el-icon class="palette-search"><Search /></el-icon>
        <input
          ref="inputRef"
          v-model="query"
          class="palette-input"
          type="text"
          role="combobox"
          aria-expanded="true"
          :aria-label="t('palette.placeholder')"
          :placeholder="t('palette.placeholder')"
          @keydown.down.prevent="move(1)"
          @keydown.up.prevent="move(-1)"
          @keydown.enter.prevent="execute(activeItem)"
        />
      </div>
      <div ref="listRef" class="palette-list" role="listbox">
        <template v-for="group in visibleGroups" :key="group.key">
          <p class="palette-group">{{ group.label }}</p>
          <button
            v-for="item in group.items"
            :key="item.id"
            type="button"
            role="option"
            :class="['palette-item', { 'is-active': item.flatIndex === activeIndex }]"
            :aria-selected="item.flatIndex === activeIndex"
            @click="execute(item)"
            @mouseenter="activeIndex = item.flatIndex"
          >
            <el-icon><component :is="item.icon" /></el-icon>
            <span class="palette-item-label">{{ item.label }}</span>
          </button>
        </template>
        <p v-if="visibleGroups.length === 0" class="palette-empty">{{ t('palette.empty') }}</p>
      </div>
      <div class="palette-foot">{{ t('palette.hint') }}</div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useEventListener } from '@vueuse/core'
import { ChatDotRound, CirclePlus, EditPen, FolderAdd, FolderOpened, Search, VideoPlay } from '@element-plus/icons-vue'
import { NAV_ITEMS, filterNavByRole } from '@/config/navigation'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import { useAskDialog } from '@/composables/useAskDialog'
import { useCommandPalette } from '@/composables/useCommandPalette'

// 命令面板（结构改版 R2 §3.1）：Ctrl/⌘+K 全局唤起，页面/项目/动作三类，纯前端数据源
// （navigation.ts NAV_ITEMS + project store 缓存，零后端改动、零全文搜索）。
// 全部条目均为纯跳转/打开既有 dialog——输入文本只用于过滤，不当命令执行（§12 安全说明）。

type PaletteItem = {
  id: string
  label: string
  /** 过滤匹配串：label（当前语言）+ 路径段 */
  match: string
  icon: Component
  run: () => void
  flatIndex: number
}

type ActionDef = {
  labelKey: string
  icon: Component
  /** 与 NAV_ITEMS 同一 minRole 语义，过滤复用 filterNavByRole 单一机制 */
  minRole?: 'maintainer'
  /** 项目列表为空时隐藏（store 无 current 兜底，方案 §3.1 依据表） */
  needsProject?: boolean
  /** 参与过滤的路径段 */
  segment: string
  run: () => void
}

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const { openAskDialog } = useAskDialog()
const { paletteOpen, closePalette, togglePalette } = useCommandPalette()

const query = ref('')
const activeIndex = ref(0)
const inputRef = ref<HTMLInputElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

// Ctrl/⌘+K 唤起/关闭：仅拦截该组合键并 preventDefault；其余按键（含输入框内普通输入）一律不拦截
useEventListener(window, 'keydown', (e: KeyboardEvent) => {
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    togglePalette()
  }
})

// 页面组：全 15 项 NAV_ITEMS 经角色过滤（含系统组与 mobile 项——桌面面板里 settings 同样可达）
const pageItems = computed<PaletteItem[]>(() =>
  filterNavByRole(NAV_ITEMS, auth.user?.role ?? '').map((i) => ({
    id: `page:${i.path}`,
    label: t(i.titleKey),
    match: `${t(i.titleKey)} ${i.path}`,
    icon: i.icon,
    run: () => router.push(i.path),
    flatIndex: -1
  }))
)

// 项目组：project store 只读缓存（AppLayout onMounted 已 load），点击跳 /projects/:id，不回写
const projectItems = computed<PaletteItem[]>(() =>
  projects.projects.map((p) => ({
    id: `project:${p.id}`,
    label: p.name,
    match: `${p.name} ${p.code} ${p.short_name}`,
    icon: FolderOpened,
    run: () => router.push(`/projects/${p.id}`),
    flatIndex: -1
  }))
)

// 动作组（§3.1 依据表）：写日报/新建项目/新建批次 minRole=maintainer
// （DailyReportView canSubmit、viewer-permission.spec「新建项目」、RunListView canEdit），
// 新建待办/AI 问答全角色；新建批次在项目列表为空时隐藏。
const actionItems = computed<PaletteItem[]>(() => {
  const defs: ActionDef[] = [
    { labelKey: 'palette.actions.dailyReport', icon: EditPen, minRole: 'maintainer', segment: 'daily-report', run: () => router.push('/daily-report') },
    { labelKey: 'palette.actions.todo', icon: CirclePlus, segment: 'todos', run: () => router.push('/todos') },
    { labelKey: 'palette.actions.ask', icon: ChatDotRound, segment: '', run: () => openAskDialog() },
    { labelKey: 'palette.actions.project', icon: FolderAdd, minRole: 'maintainer', segment: 'projects', run: () => router.push('/projects') },
    {
      labelKey: 'palette.actions.run',
      icon: VideoPlay,
      minRole: 'maintainer',
      needsProject: true,
      segment: 'experiment-runs',
      run: () => {
        const current = projects.current
        if (current) router.push(`/projects/${current.id}/experiment-runs`)
      }
    }
  ]
  return filterNavByRole(defs, auth.user?.role ?? '')
    .filter((d) => !d.needsProject || projects.projects.length > 0)
    .map((d) => ({
      id: `action:${d.labelKey}`,
      label: t(d.labelKey),
      match: `${t(d.labelKey)} ${d.segment}`,
      icon: d.icon,
      run: d.run,
      flatIndex: -1
    }))
})

const GROUP_LIMIT = 8

// 过滤：输入串对组内 label 与路径段大小写不敏感 includes；空输入显示全部，每组限量 8 条。
// 空组整组隐藏；flatIndex 为键盘导航用的跨组平铺序号。
const visibleGroups = computed(() => {
  const q = query.value.trim().toLowerCase()
  const groups = [
    { key: 'page', label: t('palette.groups.page'), items: pageItems.value },
    { key: 'project', label: t('palette.groups.project'), items: projectItems.value },
    { key: 'action', label: t('palette.groups.action'), items: actionItems.value }
  ]
  const visible = groups
    .map((g) => ({
      ...g,
      items: (q ? g.items.filter((i) => i.match.toLowerCase().includes(q)) : g.items).slice(0, GROUP_LIMIT)
    }))
    .filter((g) => g.items.length > 0)
  let idx = 0
  for (const g of visible) {
    for (const item of g.items) {
      item.flatIndex = idx
      idx += 1
    }
  }
  return visible
})

const flatItems = computed(() => visibleGroups.value.flatMap((g) => g.items))
const activeItem = computed(() => flatItems.value[activeIndex.value])

watch(query, () => {
  activeIndex.value = 0
})

// 每次打开重置输入与高亮；Esc/点击遮罩关闭走 el-dialog 既有行为（close-on-press-escape 默认开）
watch(paletteOpen, (open) => {
  if (open) {
    query.value = ''
    activeIndex.value = 0
  }
})

function onOpened() {
  inputRef.value?.focus()
}

function move(delta: number) {
  const n = flatItems.value.length
  if (n === 0) return
  activeIndex.value = (activeIndex.value + delta + n) % n
  nextTick(() => {
    listRef.value?.querySelector('.palette-item.is-active')?.scrollIntoView({ block: 'nearest' })
  })
}

function execute(item?: PaletteItem) {
  if (!item) return
  closePalette()
  item.run()
}
</script>

<style scoped>
.palette-box {
  display: flex;
  flex-direction: column;
  max-height: 64vh;
}

.palette-input-row {
  align-items: center;
  border-bottom: 1px solid var(--border);
  display: flex;
  gap: 10px;
  padding: 14px 16px;
}

.palette-search {
  color: var(--text-3);
  flex-shrink: 0;
  font-size: 16px;
}

/* 焦点环由 base.css 全局 :focus-visible 规则提供，此处不覆盖 outline（可访问性约定） */
.palette-input {
  background: transparent;
  border: none;
  color: var(--text-1);
  flex: 1;
  font-family: inherit;
  font-size: 15px;
  min-width: 0;
  padding: 0;
}

.palette-input::placeholder {
  color: var(--text-3);
}

.palette-list {
  overflow-y: auto;
  padding: 8px;
}

.palette-group {
  color: var(--text-3);
  font-size: 12px;
  letter-spacing: 0.06em;
  margin: 8px 8px 4px;
}

.palette-item {
  align-items: center;
  background: none;
  border: none;
  border-radius: var(--radius-sm);
  color: var(--text-1);
  cursor: pointer;
  display: flex;
  font-family: inherit;
  font-size: 14px;
  gap: 10px;
  padding: 9px 10px;
  text-align: left;
  width: 100%;
}

.palette-item .el-icon {
  color: var(--text-3);
  flex-shrink: 0;
}

.palette-item.is-active {
  background: var(--surface-2);
}

.palette-item.is-active .el-icon {
  color: var(--brand-500);
}

.palette-item-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.palette-empty {
  color: var(--text-3);
  margin: 0;
  padding: 24px;
  text-align: center;
}

.palette-foot {
  border-top: 1px solid var(--border);
  color: var(--text-3);
  font-size: 12px;
  padding: 8px 16px;
}
</style>

<!-- 弹窗外壳（header/body）为 EP 内部渲染，不携带本组件 data-v（scoped 落不到）；
     以 .command-palette 类为作用域前缀的非 scoped 块覆盖，选择器前缀保证不影响其他 el-dialog -->
<style>
/* 无 header 弹窗形态（方案 §3.1 结构图）：隐藏默认头部，内容自管理内边距 */
.command-palette .el-dialog__header {
  display: none;
}

.command-palette .el-dialog__body {
  padding: 0;
}
</style>
