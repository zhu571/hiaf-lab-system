import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import NotificationCenter from '@/components/business/NotificationCenter.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAgentPending } from '@/composables/useAgentPending'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/api/auth'
import type { Todo } from '@/api/todos'
import type { AlertRecord } from '@/api/alerts'

// NotificationCenter 组件测试（结构改版 R2 §3.2）：
// 三数据源只读口径（todos 今日 open / alerts active limit 6 / 待审单例计数）、badge 总数口径、
// 三组渲染与空态、viewer 无待审组、查看全部跳转并关闭、移动端 el-drawer 形态。
// ElPopover 打桩（ElDropdown 同款 jsdom 递归更新问题先例，见 appLayout.test.ts）：
// 桩件渲染 reference 槽与 visible 时内容槽，点击 reference 即打开并抛 show 事件。

vi.mock('@/api/todos', () => ({ listTodos: vi.fn() }))
vi.mock('@/api/alerts', () => ({ listAlerts: vi.fn() }))
vi.mock('@/api/agent', () => ({ listAgentCandidates: vi.fn() }))

const pushMock = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock })
}))

const mobileState = vi.hoisted(() => ({ value: false }))
vi.mock('@/composables/useMobile', async () => {
  const { computed } = await import('vue')
  return { useMobile: () => computed(() => mobileState.value) }
})

import { listTodos } from '@/api/todos'
import { listAlerts } from '@/api/alerts'
import { listAgentCandidates } from '@/api/agent'

function makeUser(role: string): UserInfo {
  return {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
}

function makeTodo(id: string, title: string, priority: Todo['priority'] = 'high'): Todo {
  return {
    id,
    title,
    priority,
    status: 'pending',
    source: 'manual',
    created_by: 'user_01',
    created_for: 'user_01',
    created_at: '2026-08-16T08:00:00+08:00',
    updated_at: '2026-08-16T08:00:00+08:00',
    owner_display_name: 'Test User'
  }
}

function makeAlert(id: string, title: string, level: AlertRecord['level'] = 'critical'): AlertRecord {
  return {
    id,
    level,
    source: 'watchdog',
    title,
    detail: '',
    status: 'active',
    occurrence_count: 1,
    first_seen: '2026-08-16T08:00:00+08:00',
    last_seen: '2026-08-16T08:00:00+08:00',
    resolved_by: '',
    created_at: '2026-08-16T08:00:00+08:00'
  }
}

// 与组件内 todayStr 同口径（本地时区 YYYY-MM-DD）
function todayStr() {
  const d = new Date()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

async function mountCenter(role: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = makeUser(role)
  const wrapper = mount(NotificationCenter, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: {
        teleport: true,
        ElPopover: {
          props: ['visible'],
          emits: ['update:visible', 'show'],
          template:
            '<div class="el-popover-stub">' +
            '<span class="popover-reference" @click="$emit(\'update:visible\', true); $emit(\'show\')"><slot name="reference" /></span>' +
            '<div v-if="visible" class="popover-content"><slot /></div>' +
            '</div>'
        }
      }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listTodos).mockReset().mockResolvedValue([])
  vi.mocked(listAlerts).mockReset().mockResolvedValue({ items: [], total: 0, limit: 6, offset: 0 })
  vi.mocked(listAgentCandidates).mockReset().mockResolvedValue({ items: [], total: 0, page: 1, per_page: 1 })
  pushMock.mockReset()
  mobileState.value = false
  // 模块级单例计数跨用例共享，前置清零防串扰（useAgentPending 依赖 active pinia，先建）
  setActivePinia(createPinia())
  useAgentPending().agentPending.value = 0
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('NotificationCenter 数据口径与角标', () => {
  it('挂载即只读拉取三组既有端点，badge 总数 = 今日待办 + active 告警 + 待审（admin）', async () => {
    vi.mocked(listTodos).mockResolvedValue([makeTodo('t1', '检查真空计读数'), makeTodo('t2', '记录气压', 'low')])
    vi.mocked(listAlerts).mockResolvedValue({ items: [makeAlert('a1', '气源压力低限告警')], total: 3, limit: 6, offset: 0 })
    vi.mocked(listAgentCandidates).mockResolvedValue({ items: [], total: 4, page: 1, per_page: 1 })
    const wrapper = await mountCenter('admin')

    expect(listTodos).toHaveBeenCalledWith({ date: todayStr(), scope: 'all', status: 'open' })
    expect(listAlerts).toHaveBeenCalledWith({ status: 'active', limit: 6 })
    expect(listAgentCandidates).toHaveBeenCalledWith({ status: 'pending_review', page: 1, per_page: 1 })
    await flushPromises()
    expect(wrapper.find('.notify-badge .el-badge__content').text()).toBe('9')
    wrapper.unmount()
  })

  it('viewer：待审组不计入 badge、不发起待审拉取', async () => {
    vi.mocked(listTodos).mockResolvedValue([makeTodo('t1', '检查真空计读数')])
    const wrapper = await mountCenter('viewer')

    expect(listAgentCandidates).not.toHaveBeenCalled()
    await flushPromises()
    expect(wrapper.find('.notify-badge .el-badge__content').text()).toBe('1')
    wrapper.unmount()
  })

  it('总数为 0 时角标隐藏', async () => {
    const wrapper = await mountCenter('admin')
    await flushPromises()
    expect(wrapper.find('.notify-badge .el-badge__content').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('NotificationCenter 面板', () => {
  it('铃铛打开面板：三组渲染（标题/计数/条目/优先级与级别标签），条目与查看全部可跳转', async () => {
    vi.mocked(listTodos).mockResolvedValue([makeTodo('t1', '检查真空计读数')])
    vi.mocked(listAlerts).mockResolvedValue({ items: [makeAlert('a1', '气源压力低限告警')], total: 1, limit: 6, offset: 0 })
    const wrapper = await mountCenter('admin')

    await wrapper.find('.notify-trigger').trigger('click')
    await flushPromises()

    const panel = wrapper.find('.notify-panel')
    expect(panel.exists()).toBe(true)
    expect(panel.text()).toContain('待办（今日）')
    expect(panel.text()).toContain('活跃告警')
    expect(panel.text()).toContain('待审候选')
    expect(panel.text()).toContain('检查真空计读数')
    expect(panel.text()).toContain('气源压力低限告警')
    // 优先级/级别标签走 StatusBadge（statusMeta 注册表：todoPriority/alertLevel）
    expect(panel.findAll('.notify-item .el-tag').length).toBe(2)
    expect(panel.text()).toContain('高')
    expect(panel.text()).toContain('严重')

    // 条目点击 → 跳待办页并关闭面板
    await panel.find('.notify-item').trigger('click')
    await flushPromises()
    expect(pushMock).toHaveBeenCalledWith('/todos')
    expect(wrapper.find('.notify-panel').exists()).toBe(false)

    // 查看全部：告警组 → /alerts
    await wrapper.find('.notify-trigger').trigger('click')
    await flushPromises()
    const viewAlls = wrapper.findAll('.notify-viewall')
    await viewAlls[1].trigger('click')
    await flushPromises()
    expect(pushMock).toHaveBeenCalledWith('/alerts')
    wrapper.unmount()
  })

  it('viewer：面板无待审组', async () => {
    vi.mocked(listTodos).mockResolvedValue([makeTodo('t1', '检查真空计读数')])
    const wrapper = await mountCenter('viewer')
    await wrapper.find('.notify-trigger').trigger('click')
    await flushPromises()
    const panel = wrapper.find('.notify-panel')
    expect(panel.text()).toContain('待办（今日）')
    expect(panel.text()).toContain('活跃告警')
    expect(panel.text()).not.toContain('待审候选')
    wrapper.unmount()
  })

  it('全空显示 el-empty；单组空显示「暂无」行', async () => {
    // 全空
    const wrapper = await mountCenter('admin')
    await wrapper.find('.notify-trigger').trigger('click')
    await flushPromises()
    expect(wrapper.find('.notify-panel .el-empty').exists()).toBe(true)
    expect(wrapper.find('.notify-panel').text()).toContain('暂无通知')
    expect(wrapper.findAll('.notify-group')).toHaveLength(0)
    wrapper.unmount()

    // 单组空：仅告警有数据 → 待办组「暂无」行，不显示全空 el-empty
    vi.mocked(listAlerts).mockResolvedValue({ items: [makeAlert('a1', '气源压力低限告警')], total: 1, limit: 6, offset: 0 })
    const wrapper2 = await mountCenter('admin')
    await wrapper2.find('.notify-trigger').trigger('click')
    await flushPromises()
    expect(wrapper2.find('.notify-panel .el-empty').exists()).toBe(false)
    expect(wrapper2.findAll('.notify-empty-row').length).toBeGreaterThan(0)
    expect(wrapper2.find('.notify-panel').text()).toContain('暂无')
    wrapper2.unmount()
  })

  it('移动端形态：el-drawer 容器，点击铃铛打开抽屉渲染面板', async () => {
    mobileState.value = true
    vi.mocked(listTodos).mockResolvedValue([makeTodo('t1', '检查真空计读数')])
    const wrapper = await mountCenter('admin')
    expect(wrapper.find('.el-popover-stub').exists()).toBe(false)

    await wrapper.find('.notify-trigger').trigger('click')
    await flushPromises()
    expect(wrapper.find('.notify-drawer').exists()).toBe(true)
    const panel = wrapper.find('.notify-panel')
    expect(panel.exists()).toBe(true)
    expect(panel.text()).toContain('检查真空计读数')
    wrapper.unmount()
  })
})
