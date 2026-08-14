import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { ElTag } from 'element-plus'
import StatusBadge from '@/components/base/StatusBadge.vue'

// T0 示范用例：打通 SFC 编译（vue 插件）+ Element Plus 全量注册（setup.ts）+ 组件 API 断言。
// 断言面向组件 API（props）与文本，不断言 element-plus 内部 DOM 结构。
describe('StatusBadge', () => {
  it('success 组：active/published/confirmed/resolved 映射为 success 类型并渲染 label', () => {
    for (const value of ['active', 'published', 'confirmed', 'resolved']) {
      const wrapper = mount(StatusBadge, { props: { value } })
      expect(wrapper.findComponent(ElTag).props('type'), `value=${value} 应为 success`).toBe('success')
      expect(wrapper.text()).toBe(value)
    }
  })

  it('warning 组与 info 组：draft/candidate/open → warning；archived/closed/locked → info', () => {
    for (const value of ['draft', 'candidate', 'open']) {
      const wrapper = mount(StatusBadge, { props: { value } })
      expect(wrapper.findComponent(ElTag).props('type'), `value=${value} 应为 warning`).toBe('warning')
    }
    for (const value of ['archived', 'closed', 'locked']) {
      const wrapper = mount(StatusBadge, { props: { value } })
      expect(wrapper.findComponent(ElTag).props('type'), `value=${value} 应为 info`).toBe('info')
    }
  })

  it('未知值兜底为 primary，label 将下划线替换为空格', () => {
    const wrapper = mount(StatusBadge, { props: { value: 'in_progress' } })
    expect(wrapper.findComponent(ElTag).props('type')).toBe('primary')
    expect(wrapper.text()).toBe('in progress')
  })
})
