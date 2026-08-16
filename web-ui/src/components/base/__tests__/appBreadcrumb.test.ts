import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AppBreadcrumb from '@/components/base/AppBreadcrumb.vue'
import { createTestI18n } from '@/test-utils/setup'

// AppBreadcrumb 组件测试（结构改版 R1 §2.3 契约）：
// 纯 props items { label, to? }[]；单段纯文本不可点；≥2 段父段可点 + 分隔符；末段恒为当前页纯文本；
// 空 items 整体不渲染。

function mountBreadcrumb(items: { label: string; to?: string }[]) {
  return mount(AppBreadcrumb, {
    props: { items },
    global: {
      plugins: [createTestI18n()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
    }
  })
}

describe('AppBreadcrumb 渲染规则', () => {
  it('空 items 不渲染', () => {
    const wrapper = mountBreadcrumb([])
    expect(wrapper.find('.app-breadcrumb').exists()).toBe(false)
  })

  it('单段渲染为纯标题文本，无链接无分隔符', () => {
    const wrapper = mountBreadcrumb([{ label: '待办' }])
    expect(wrapper.findAll('.crumb-item')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('待办')
    expect(wrapper.find('.crumb-link').exists()).toBe(false)
    expect(wrapper.find('.crumb-sep').exists()).toBe(false)
    expect(wrapper.find('.crumb-text').attributes('aria-current')).toBe('page')
  })

  it('两段：父段渲染为可点链接，末段为当前页纯文本，含分隔符', () => {
    const wrapper = mountBreadcrumb([
      { label: '日报', to: '/daily-report' },
      { label: '日报历史' }
    ])
    const link = wrapper.find('.crumb-link')
    expect(link.exists()).toBe(true)
    expect(link.text()).toBe('日报')
    expect(link.attributes('href')).toBe('/daily-report')
    expect(wrapper.find('.crumb-text').text()).toBe('日报历史')
    expect(wrapper.findAll('.crumb-sep')).toHaveLength(1)
  })

  it('末段即使携带 to 也不渲染为链接（当前页恒纯文本）', () => {
    const wrapper = mountBreadcrumb([
      { label: '项目', to: '/projects' },
      { label: 'HIAF气靶', to: '/projects/p1' }
    ])
    expect(wrapper.findAll('.crumb-link')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('HIAF气靶')
  })

  it('三段：仅父段可点，分隔符数量为段数减一', () => {
    const wrapper = mountBreadcrumb([
      { label: 'A', to: '/a' },
      { label: 'B', to: '/a/b' },
      { label: 'C' }
    ])
    expect(wrapper.findAll('.crumb-link')).toHaveLength(2)
    expect(wrapper.findAll('.crumb-sep')).toHaveLength(2)
    expect(wrapper.find('.crumb-text').text()).toBe('C')
  })
})
