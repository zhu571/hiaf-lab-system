import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElEmpty } from 'element-plus'
import StepItemsEditor from '@/components/business/StepItemsEditor.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { StepTemplateItem } from '@/api/stepTemplates'

// StepItemsEditor 组件测试（测试方案 §3.2 🔴 深测）：
// 纯 props/emits 交互组件——增/删/移 + 依赖序号重排 + MAX_ITEMS=30 上限。
// 断言面向 emits 载荷与语义文本（按钮文案 i18n），不绑定 element-plus 内部 DOM。
// 注意：el-table 列在挂载后异步注册（列宽测量），行内控件需 flushPromises 后才可交互。

function makeItem(overrides: Partial<StepTemplateItem> = {}): StepTemplateItem {
  return { name: '步骤A', description: '', step_order: 1, depends_on_order: null, ...overrides }
}

async function mountEditor(modelValue: StepTemplateItem[]) {
  const wrapper = mount(StepItemsEditor, {
    props: { modelValue },
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

function emittedPayload(wrapper: Awaited<ReturnType<typeof mountEditor>>, index = 0): StepTemplateItem[] {
  return wrapper.emitted('update:modelValue')![index][0] as StepTemplateItem[]
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('StepItemsEditor 渲染', () => {
  it('按 step_order 升序渲染行，行号/名称/描述正确', async () => {
    const wrapper = await mountEditor([
      makeItem({ name: 'C', step_order: 3, depends_on_order: 1 }),
      makeItem({ name: 'A', step_order: 1 }),
      makeItem({ name: 'B', step_order: 2, description: '中间步骤' })
    ])
    const rows = wrapper.findAll('tbody tr')
    expect(rows.length).toBeGreaterThanOrEqual(3)
    // 每行首列 input 为名称（描述/依赖列在后续列）
    const nameOf = (i: number) => rows[i].findAll('input')[0].element.value
    expect(nameOf(0)).toBe('A')
    expect(nameOf(1)).toBe('B')
    expect(nameOf(2)).toBe('C')
    // 第二列 input 为描述
    expect(rows[1].findAll('input')[1].element.value).toBe('中间步骤')
  })

  it('空列表显示 el-empty 空态；添加按钮可用', async () => {
    const wrapper = await mountEditor([])
    expect(wrapper.findComponent(ElEmpty).exists()).toBe(true)
    expect(wrapper.find('.add-btn').attributes('disabled')).toBeUndefined()
  })
})

describe('StepItemsEditor 交互', () => {
  it('新增行：emit update:modelValue 载荷追加空白行并整体重排 step_order', async () => {
    const wrapper = await mountEditor([makeItem({ name: 'A', step_order: 1 })])
    await wrapper.find('.add-btn').trigger('click')
    const payload = emittedPayload(wrapper)
    expect(payload).toHaveLength(2)
    expect(payload.map((s) => s.step_order)).toEqual([1, 2])
    expect(payload[1].name).toBe('')
    expect(payload[1].description).toBeUndefined()
  })

  it('编辑名称输入：emit 载荷同步新值', async () => {
    const wrapper = await mountEditor([makeItem({ name: 'A', step_order: 1, description: '旧描述' })])
    const input = wrapper.findAll('input')[0]
    await input.setValue('新名称')
    const payload = emittedPayload(wrapper)
    expect(payload[0].name).toBe('新名称')
    expect(payload[0].step_order).toBe(1)
  })

  it('删除行：depends_on_order 重编号——引用被删行 → null，引用后续行 → 自减', async () => {
    // 场景 1：C 引用被删的 B（order=2）→ 置 null
    const wrapper = await mountEditor([
      makeItem({ name: 'A', step_order: 1, depends_on_order: null }),
      makeItem({ name: 'B', step_order: 2, depends_on_order: 1 }),
      makeItem({ name: 'C', step_order: 3, depends_on_order: 2 })
    ])
    let deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === '删除')
    await deleteButtons[1].trigger('click')
    let payload = emittedPayload(wrapper)
    expect(payload.map((s) => s.name)).toEqual(['A', 'C'])
    expect(payload.map((s) => s.step_order)).toEqual([1, 2])
    expect(payload[0].depends_on_order).toBeNull()
    expect(payload[1].depends_on_order).toBeNull()

    // 场景 2：A 引用被删的 B（order=2）之后的步骤 3 → 3 > 2 自减为 2
    const wrapper2 = await mountEditor([
      makeItem({ name: 'A', step_order: 1, depends_on_order: 3 }),
      makeItem({ name: 'B', step_order: 2 }),
      makeItem({ name: 'C', step_order: 3 })
    ])
    deleteButtons = wrapper2.findAll('button').filter((b) => b.text().trim() === '删除')
    await deleteButtons[1].trigger('click')
    payload = emittedPayload(wrapper2)
    expect(payload.map((s) => s.name)).toEqual(['A', 'C'])
    expect(payload[0].depends_on_order).toBe(2)
    expect(payload[1].depends_on_order).toBeNull()
  })

  it('上移/下移：emit 载荷行序交换，depends_on_order 序号引用跟随交换', async () => {
    const wrapper = await mountEditor([
      makeItem({ name: 'A', step_order: 1, depends_on_order: 2 }),
      makeItem({ name: 'B', step_order: 2, depends_on_order: null }),
      makeItem({ name: 'C', step_order: 3, depends_on_order: 1 })
    ])
    // B 上移一位：A/B 交换，depends_on_order 序号引用（2↔1）跟随交换
    const upButtons = wrapper.findAll('button').filter((b) => b.text().trim() === '上移')
    await upButtons[1].trigger('click')
    let payload = emittedPayload(wrapper)
    expect(payload.map((s) => s.name)).toEqual(['B', 'A', 'C'])
    expect(payload.map((s) => s.step_order)).toEqual([1, 2, 3])
    expect(payload[0].depends_on_order).toBeNull()
    expect(payload[1].depends_on_order).toBe(1)
    expect(payload[2].depends_on_order).toBe(2)
    // B 下移一位：B/C 交换，depends_on_order 序号引用（2↔1）跟随交换
    const downButtons = wrapper.findAll('button').filter((b) => b.text().trim() === '下移')
    await downButtons[0].trigger('click')
    payload = emittedPayload(wrapper, 1)
    expect(payload.map((s) => s.name)).toEqual(['A', 'B', 'C'])
    expect(payload[0].depends_on_order).toBe(2)
    expect(payload[1].depends_on_order).toBeNull()
    expect(payload[2].depends_on_order).toBe(1)
  })
})

describe('StepItemsEditor 边界', () => {
  it('MAX_ITEMS=30：满 30 行时添加按钮禁用，点击不 emit；首行上移/末行下移禁用', async () => {
    const items = Array.from({ length: 30 }, (_, i) => makeItem({ name: `S${i + 1}`, step_order: i + 1 }))
    const wrapper = await mountEditor(items)
    expect(wrapper.find('.add-btn').attributes('disabled')).toBeDefined()
    await wrapper.find('.add-btn').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const buttons = wrapper.findAll('button')
    const upButtons = buttons.filter((b) => b.text().trim() === '上移')
    const downButtons = buttons.filter((b) => b.text().trim() === '下移')
    expect(upButtons[0].attributes('disabled')).toBeDefined()
    expect(downButtons[downButtons.length - 1].attributes('disabled')).toBeDefined()
  })
})
