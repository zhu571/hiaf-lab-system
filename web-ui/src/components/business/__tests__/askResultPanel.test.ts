import { describe, it, expect } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AskResultPanel from '@/components/business/AskResultPanel.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { AskResultData } from '@/api/ask'

// AskResultPanel 组件测试（测试方案 §3.2 🔴 深测）：
// 表格渲染、按 canOpenRow 显隐「查看详情」跳转按钮（配合 askRoutes 单测）。

function makeData(overrides: Partial<AskResultData> = {}): AskResultData {
  return {
    answer: '查询到 2 条记录',
    sql: 'SELECT * FROM test_data LIMIT 2',
    tableName: 'test_data',
    columns: ['id', 'value'],
    rows: [
      { id: 'td_1', value: 12.5 },
      { id: 'td_2', value: 13.7 }
    ],
    rowCount: 2,
    truncated: false,
    durationMs: 120,
    ...overrides
  }
}

async function mountPanel(data: AskResultData) {
  const wrapper = mount(AskResultPanel, {
    props: { data },
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

describe('AskResultPanel 渲染', () => {
  it('answer 经 MarkdownView 渲染到 answer-box', async () => {
    const wrapper = await mountPanel(makeData())
    expect(wrapper.find('.answer-box').exists()).toBe(true)
    expect(wrapper.text()).toContain('查询到 2 条记录')
  })

  it('有 rows 时渲染数据表格：动态列、表名、行数、耗时、截断标记；空结果不渲染表格', async () => {
    const wrapper = await mountPanel(
      makeData({ truncated: true, durationMs: 250, tableName: 'experiment_runs' })
    )
    const table = wrapper.find('.data-table')
    expect(table.exists()).toBe(true)
    expect(wrapper.find('.table-name').text()).toBe('experiment_runs')
    expect(wrapper.text()).toContain('2 行')
    expect(wrapper.text()).toContain('耗时 250 ms')
    expect(wrapper.text()).toContain('结果已截断')
    expect(wrapper.find('.sql-text').text()).toContain('SELECT')

    // 空结果：不渲染表格与 SQL 块，仅显示 answer
    const empty = await mountPanel(makeData({ rows: [], rowCount: 0 }))
    expect(empty.find('.data-table').exists()).toBe(false)
    expect(empty.find('.sql-box').exists()).toBe(false)
    expect(empty.find('.answer-box').exists()).toBe(true)
  })
})

describe('AskResultPanel 查看详情', () => {
  it('canOpenRow 命中（行含 id）渲染查看详情按钮，点击 emit open 路由；行缺 id 时按钮隐藏', async () => {
    // detail 型表（experiment_runs）需行含 id
    const wrapper = await mountPanel(
      makeData({
        tableName: 'experiment_runs',
        columns: ['id', 'name'],
        rows: [
          { id: 'run_1', name: '运行一' },
          { name: '运行二' }
        ]
      })
    )
    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].text()).toContain('查看详情')
    await buttons[0].trigger('click')
    expect(wrapper.emitted('open')).toEqual([['/experiment-runs/run_1']])
  })

  it('无映射表（logs）不渲染详情列；project 型表缺 project_id 时按钮隐藏', async () => {
    const noRoute = await mountPanel(makeData({ tableName: 'logs', rows: [{ id: 'l_1' }] }))
    expect(noRoute.findAll('button')).toHaveLength(0)

    const missingPid = await mountPanel(makeData({ tableName: 'issues', rows: [{ id: 'i_1' }] }))
    expect(missingPid.findAll('button')).toHaveLength(0)
  })
})
