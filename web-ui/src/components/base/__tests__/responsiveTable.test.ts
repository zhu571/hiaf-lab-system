import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElTable } from 'element-plus'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { createTestI18n } from '@/test-utils/setup'

// ResponsiveTable 组件测试（测试方案 §3.2 🔴 深测）：
// rows 渲染、loading、empty 插槽、card 插槽（移动端分支）。
// 桌面/移动分支由 matchMedia stub 控制（setup.ts 默认不匹配 → 桌面）。

function setMobile(matches: boolean) {
  window.matchMedia = ((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false
  })) as unknown as typeof window.matchMedia
}

async function mountTable(opts: { mobile?: boolean } = {}) {
  setMobile(!!opts.mobile)
  const wrapper = mount(ResponsiveTable, {
    props: { rows: [{ id: 'a', name: '甲' }, { id: 'b', name: '乙' }], loading: false },
    slots: {
      default: '<el-table-column prop="name" label="名称" />',
      card: `<template #card="{ row }"><span class="card-title">{{ row.name }}</span></template>`
    },
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ResponsiveTable 桌面分支', () => {
  it('rows 传入 el-table 渲染，列插槽正常', async () => {
    const wrapper = await mountTable()
    expect(wrapper.findComponent(ElTable).exists()).toBe(true)
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('甲')
    expect(wrapper.find('.rt-card-list').exists()).toBe(false)
  })

  it('loading 时 v-loading 遮罩出现；无 card 插槽时移动端也走表格', async () => {
    const wrapper = await mountTable()
    await wrapper.setProps({ loading: true })
    await flushPromises()
    expect(wrapper.find('.el-loading-mask').exists()).toBe(true)
    // 无 card 插槽（default 只有表格列）→ 移动端仍渲染 el-table
    setMobile(true)
    const noCard = mount(ResponsiveTable, {
      props: { rows: [{ id: 'a', name: '甲' }] },
      slots: { default: '<el-table-column prop="name" label="名称" />' },
      global: { plugins: [createTestI18n()] }
    })
    await flushPromises()
    expect(noCard.findComponent(ElTable).exists()).toBe(true)
  })

  it('空 rows 渲染 empty 插槽内容', async () => {
    setMobile(false)
    const wrapper = mount(ResponsiveTable, {
      props: { rows: [], loading: false },
      slots: { empty: '<p class="custom-empty">暂无数据</p>' },
      global: { plugins: [createTestI18n()] }
    })
    await flushPromises()
    expect(wrapper.find('.custom-empty').exists()).toBe(true)
  })
})

describe('ResponsiveTable 移动端分支', () => {
  it('card 插槽存在时移动端渲染卡片列表；空列表展示 empty 插槽', async () => {
    const wrapper = await mountTable({ mobile: true })
    expect(wrapper.find('.rt-card-list').exists()).toBe(true)
    const cards = wrapper.findAll('.rt-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('甲')
    expect(wrapper.findComponent(ElTable).exists()).toBe(false)

    const emptyMobile = mount(ResponsiveTable, {
      props: { rows: [], loading: false },
      slots: {
        card: '<template #card="{ row }"><span>{{ row.name }}</span></template>',
        empty: '<p class="custom-empty">暂无数据</p>'
      },
      global: { plugins: [createTestI18n()] }
    })
    await flushPromises()
    expect(emptyMobile.find('.custom-empty').exists()).toBe(true)
  })
})
