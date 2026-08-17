import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import KanbanBoard from '@/components/base/KanbanBoard.vue'
import { createTestI18n } from '@/test-utils/setup'

// KanbanBoard 组件测试（结构改版 R5 §6.2 契约）：
// 列渲染（panel.column + column-head + count 派生/覆盖）、tone 圆点色、
// card 槽按列内 items 逐项调用并透传 column/item、empty 槽仅空列渲染。

interface Row {
  id: string
  title: string
}

const columns: { key: string; label: string; tone?: string; items: Row[]; count?: number }[] = [
  { key: 'open', label: '打开', tone: '--danger', items: [{ id: 'i1', title: '甲' }, { id: 'i2', title: '乙' }] },
  { key: 'done', label: '完成', items: [] }
]

function mountBoard(props: { columns: typeof columns } = { columns }) {
  return mount(KanbanBoard, {
    props,
    slots: {
      card: '<template #card="{ column, item }"><button class="x-card" :data-col="column.key">{{ item.title }}</button></template>',
      empty: '<template #empty="{ column }">空：{{ column.key }}</template>'
    },
    global: { plugins: [createTestI18n()] }
  })
}

describe('KanbanBoard 列结构', () => {
  it('按 columns 渲染列：panel.column + 列头 label + data-status', () => {
    const wrapper = mountBoard()
    const cols = wrapper.findAll('.column')
    expect(cols).toHaveLength(2)
    expect(cols[0].attributes('data-status')).toBe('open')
    expect(cols[0].find('.column-head h3').text()).toContain('打开')
  })

  it('列头 count 由 items.length 派生；显式 count 覆盖', () => {
    const wrapper = mountBoard()
    expect(wrapper.findAll('.count')[0].text()).toBe('2')
    expect(wrapper.findAll('.count')[1].text()).toBe('0')
    const overridden = mountBoard({ columns: [{ key: 'a', label: 'A', items: [{ id: 'x', title: 'x' }], count: 42 }] })
    expect(overridden.find('.count').text()).toBe('42')
  })

  it('tone 控制列头圆点色（CSS 变量），缺省无内联色', () => {
    const wrapper = mountBoard()
    expect(wrapper.findAll('.dot')[0].attributes('style')).toContain('background: var(--danger)')
    expect(wrapper.findAll('.dot')[1].attributes('style') ?? '').not.toContain('background')
  })

  it('列数写入 --kanban-columns 供网格列模板使用', () => {
    expect(mountBoard().attributes('style')).toContain('--kanban-columns: 2')
  })
})

describe('KanbanBoard 槽', () => {
  it('card 槽按列内 items 逐项渲染，透传 column 与 item', () => {
    const wrapper = mountBoard()
    const cards = wrapper.findAll('.x-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toBe('甲')
    expect(cards[0].attributes('data-col')).toBe('open')
  })

  it('empty 槽仅在空列渲染（.empty-hint 容器）', () => {
    const wrapper = mountBoard()
    const hints = wrapper.findAll('.empty-hint')
    expect(hints).toHaveLength(1)
    expect(hints[0].text()).toBe('空：done')
  })
})
