import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { ElEmpty, ElSkeleton } from 'element-plus'
import { Tickets } from '@element-plus/icons-vue'
import DashboardPanel from '@/components/base/DashboardPanel.vue'
import { createTestI18n } from '@/test-utils/setup'

// DashboardPanel 组件测试（结构改版 R5 §6.3 契约）：
// panel-head（icon/title/meta/actions 槽）；divided 承接 DashboardView 覆写类；
// 三态门控——传入任一状态 prop 时内建 StateBlock 包裹 default 槽（loading > error > empty > 内容），
// 三者都不传时 default 槽直渲染（面板内 StateBlock 外常驻内容由接入方自管）。

function mountPanel(options: { props?: Record<string, unknown>; slots?: Record<string, string> } = {}) {
  return mount(DashboardPanel, {
    props: { title: '面板', ...options.props },
    slots: options.slots,
    global: { plugins: [createTestI18n()] }
  })
}

describe('DashboardPanel 头部', () => {
  it('title/icon/meta 渲染 panel-head 体系；无 icon/meta 不渲染对应元素', () => {
    const wrapper = mountPanel({ props: { icon: Tickets, meta: '共 3 条' } })
    expect(wrapper.find('.panel-head .panel-title').text()).toBe('面板')
    expect(wrapper.find('.panel-icon').exists()).toBe(true)
    expect(wrapper.find('.panel-meta').text()).toBe('共 3 条')
    const bare = mountPanel()
    expect(bare.find('.panel-icon').exists()).toBe(false)
    expect(bare.find('.panel-meta').exists()).toBe(false)
  })

  it('actions 槽渲染在 panel-head 内', () => {
    const wrapper = mountPanel({ slots: { actions: '<button class="act">更多</button>' } })
    expect(wrapper.find('.panel-head .act').exists()).toBe(true)
  })

  it('divided 加分隔修饰类（.panel-head 下边线 + .panel-meta 右推）', () => {
    expect(mountPanel().classes()).not.toContain('dashboard-panel--divided')
    expect(mountPanel({ props: { divided: true } }).classes()).toContain('dashboard-panel--divided')
  })
})

describe('DashboardPanel 三态门控', () => {
  it('不传状态 props：default 槽直渲染，无 StateBlock 包裹', () => {
    const wrapper = mountPanel({ slots: { default: '<div class="body">内容</div>' } })
    expect(wrapper.find('.body').exists()).toBe(true)
    expect(wrapper.findComponent(ElSkeleton).exists()).toBe(false)
  })

  it('loading：骨架优先，不渲染内容', () => {
    const wrapper = mountPanel({ props: { loading: true }, slots: { default: '<div class="body" />' } })
    expect(wrapper.findComponent(ElSkeleton).exists()).toBe(true)
    expect(wrapper.find('.body').exists()).toBe(false)
  })

  it('error：展示 errorText 与重试，点击 emit retry', async () => {
    const wrapper = mountPanel({
      props: { error: { message: 'x' }, errorText: '加载失败' },
      slots: { default: '<div class="body" />' }
    })
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('加载失败')
    await wrapper.find('.state-block-retry').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('empty：展示 emptyText，不渲染内容', () => {
    const wrapper = mountPanel({ props: { empty: true, emptyText: '暂无数据' }, slots: { default: '<div class="body" />' } })
    expect(wrapper.findComponent(ElEmpty).exists()).toBe(true)
    expect(wrapper.text()).toContain('暂无数据')
    expect(wrapper.find('.body').exists()).toBe(false)
  })

  it('内容态：状态全假时渲染 default 槽', () => {
    const wrapper = mountPanel({
      props: { loading: false, error: null, empty: false },
      slots: { default: '<div class="body">内容</div>' }
    })
    expect(wrapper.find('.body').text()).toBe('内容')
  })
})
