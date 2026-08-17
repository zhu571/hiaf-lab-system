import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CardGrid from '@/components/base/CardGrid.vue'
import { createTestI18n } from '@/test-utils/setup'

// CardGrid 组件测试（结构改版 R5 §6.1 契约）：
// default 槽渲染；min/gap/mode/columns/mobileColumns/smMin 参数经 CSS 变量注入；
// 等值替换目标——列模板/间距与五处自制网格现状值一一对应。

function mountGrid(props: Record<string, unknown> = {}, slots: Record<string, string> = { default: '<div class="cell" />' }) {
  return mount(CardGrid, { props, slots, global: { plugins: [createTestI18n()] } })
}

describe('CardGrid', () => {
  it('default 槽内容渲染在 .card-grid 容器内', () => {
    const wrapper = mountGrid()
    expect(wrapper.find('.card-grid .cell').exists()).toBe(true)
  })

  it('缺省参数：auto-fill + min 240px + gap --space-4', () => {
    const style = mountGrid().attributes('style') ?? ''
    expect(style).toContain('--card-grid-template: repeat(auto-fill, minmax(240px, 1fr))')
    expect(style).toContain('--card-grid-gap: var(--space-4)')
    expect(style).not.toContain('--card-grid-template-mobile')
    expect(style).not.toContain('--card-grid-template-sm')
  })

  it('min/mode/gap 逐视图对齐现状值（如 GasControl auto-fit 170px 14px）', () => {
    const style = mountGrid({ min: '170px', mode: 'auto-fit', gap: '14px' }).attributes('style') ?? ''
    expect(style).toContain('--card-grid-template: repeat(auto-fit, minmax(170px, 1fr))')
    expect(style).toContain('--card-grid-gap: 14px')
  })

  it('columns 完整覆盖列模板（RunList 固定 3 列），优先于 min/mode', () => {
    const style = mountGrid({ columns: 'repeat(3, minmax(0, 1fr))', min: '999px' }).attributes('style') ?? ''
    expect(style).toContain('--card-grid-template: repeat(3, minmax(0, 1fr))')
    expect(style).not.toContain('999px')
  })

  it('mobileColumns/smMin 仅在传入时注入对应断点变量', () => {
    const style = mountGrid({ min: '170px', smMin: '140px', mobileColumns: '1fr' }).attributes('style') ?? ''
    expect(style).toContain('--card-grid-template-mobile: 1fr')
    expect(style).toContain('--card-grid-template-sm: repeat(auto-fill, minmax(140px, 1fr))')
  })
})
