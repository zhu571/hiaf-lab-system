import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ManualView from '@/views/ManualView.vue'
import { createTestI18n } from '@/test-utils/setup'

// ManualView 页面测试（测试方案 §3.2 🟢 smoke + §9 排除项 6）：
// 静态文案不做内容测试，仅断言挂载 + 目录区块存在（zh/en 双语结构）。

describe('ManualView 挂载冒烟', () => {
  it('手册页渲染目录与章节区块（中文）', () => {
    const wrapper = mount(ManualView, {
      global: { plugins: [createTestI18n()] }
    })
    expect(wrapper.text()).toContain('系统手册')
    expect(wrapper.findAll('.manual-section').length).toBeGreaterThan(5)
    expect(wrapper.find('.manual-layout > .toc').exists()).toBe(true)
    const tocLinks = wrapper.findAll('.toc-chip')
    expect(tocLinks).toHaveLength(wrapper.findAll('.manual-section').length)
    for (const link of tocLinks) expect(wrapper.find(link.attributes('href')!).exists()).toBe(true)
  })
})
