import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElMessage } from 'element-plus'
import TestDataBatchEditor from '@/components/business/TestDataBatchEditor.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { ExperimentRun } from '@/api/runs'
import type { TestDataBatchRow } from '@/api/testdata'

// TestDataBatchEditor 组件测试（测试方案 §3.2 🔴 深测）：
// 初始空行、加/删/清空行、粘贴追加语义+容量截断、客户端校验拦截提交+标红、
// 后端 422 details 逐行映射、submit 成功 emit submitted。
// 行数据驱动走剪贴板粘贴（真实 parsePastedTestData 纯函数），提交走 createTestDataBatch mock。

vi.mock('@/api/testdata', () => ({
  createTestDataBatch: vi.fn()
}))

import { createTestDataBatch } from '@/api/testdata'

const runs: ExperimentRun[] = [
  { id: 'run_01', project_id: 'proj_01', name: '实验运行 01', run_type: 'experiment', status: 'planned', gas_type: 'Ar', pressure_unit: 'Pa', has_beam: false, created_at: '2026-01-02T10:00:00+08:00', updated_at: '2026-01-02T10:00:00+08:00' }
]

const msg = ElMessage as unknown as {
  success: ReturnType<typeof vi.fn>
  warning: ReturnType<typeof vi.fn>
  error: ReturnType<typeof vi.fn>
  info: ReturnType<typeof vi.fn>
}

async function mountEditor(clipboardText?: string, stubHeavyCells = false) {
  if (clipboardText !== undefined) {
    Object.defineProperty(navigator, 'clipboard', {
      value: { readText: vi.fn().mockResolvedValue(clipboardText) },
      configurable: true
    })
  }
  // stubHeavyCells：容量用例渲染 100 行，jsdom 下 el-select/el-input-number/el-date-picker
  // 每行 3 个实例过重；stub 后行结构与提交逻辑不受影响（行校验/标红断言不依赖这三者）
  const wrapper = mount(TestDataBatchEditor, {
    props: { projectId: 'proj_01', runs },
    global: {
      plugins: [createTestI18n()],
      stubs: stubHeavyCells ? { ElSelect: true, ElInputNumber: true, ElDatePicker: true } : {}
    }
  })
  await flushPromises()
  return wrapper
}

/** CSV 行文本：类型\t测量项\t数值 */
function csvRow(dataType: string, measurement: string, value: string) {
  return [dataType, measurement, value].join('\t')
}

async function pasteRows(wrapper: Awaited<ReturnType<typeof mountEditor>>, text: string) {
  Object.defineProperty(navigator, 'clipboard', {
    value: { readText: vi.fn().mockResolvedValue(text) },
    configurable: true
  })
  const pasteButton = wrapper.findAll('button').find((b) => b.text().trim() === '从剪贴板粘贴')!
  await pasteButton.trigger('click')
  await flushPromises()
}

/** 删除初始空白行（粘贴/校验用例前置：避免空白行参与客户端校验） */
async function removeInitialRow(wrapper: Awaited<ReturnType<typeof mountEditor>>) {
  const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === '删除')
  await deleteButtons[0].trigger('click')
  await flushPromises()
}

function submitButton(wrapper: Awaited<ReturnType<typeof mountEditor>>) {
  return wrapper.findAll('button').find((b) => b.text().includes('提交（'))!
}

beforeEach(() => {
  vi.mocked(createTestDataBatch).mockReset()
  msg.success.mockClear()
  msg.warning.mockClear()
  msg.error.mockClear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('TestDataBatchEditor 行管理', () => {
  it('初始渲染 1 行空白行，提交按钮显示行数', async () => {
    const wrapper = await mountEditor()
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(submitButton(wrapper).text()).toBe('提交（1 条）')
  })

  it('新增一行 / 删除一行：行数增减，提交按钮行数同步', async () => {
    const wrapper = await mountEditor()
    const addButton = wrapper.findAll('button').find((b) => b.text().trim() === '新增一行')!
    await addButton.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(submitButton(wrapper).text()).toBe('提交（2 条）')
    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === '删除')
    await deleteButtons[0].trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })

  it('清空全部行：多行时一键回到 1 行空白行', async () => {
    const wrapper = await mountEditor()
    await pasteRows(wrapper, [csvRow('pressure', 'a', '1'), csvRow('voltage', 'b', '2')].join('\n'))
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    const clearButton = wrapper.findAll('button').find((b) => b.text().trim() === '清空全部行')!
    await clearButton.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })
})

describe('TestDataBatchEditor 粘贴', () => {
  it('剪贴板粘贴：解析行追加到表格末尾，toast 成功行数', async () => {
    const wrapper = await mountEditor()
    await pasteRows(wrapper, [csvRow('pressure', '入口压强', '101325'), csvRow('voltage', 'rf_voltage', '12.5')].join('\n'))
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    expect(msg.success).toHaveBeenCalledWith(expect.stringContaining('已解析 2 行'))
  })

  it('粘贴容量截断：超过 MAX_BATCH_ROWS=100 时保留前 100 行并提示截断；再次粘贴无剩余容量时拒绝', async () => {
    const wrapper = await mountEditor(undefined, true)
    await removeInitialRow(wrapper)
    // 初始 0 行 + 102 行粘贴 → 截断到 100 行
    const bigCsv = Array.from({ length: 102 }, (_, i) => csvRow('pressure', `m${i}`, String(i))).join('\n')
    await pasteRows(wrapper, bigCsv)
    expect(wrapper.findAll('tbody tr')).toHaveLength(100)
    expect(msg.warning).toHaveBeenCalledWith(expect.stringContaining('已保留前 100 行'))
    // 已满 → 再次粘贴直接拒绝
    msg.warning.mockClear()
    await pasteRows(wrapper, [csvRow('pressure', 'x', '1'), csvRow('pressure', 'y', '2')].join('\n'))
    expect(wrapper.findAll('tbody tr')).toHaveLength(100)
    expect(msg.warning).toHaveBeenCalledWith(expect.stringContaining('单次最多提交 100 条'))
  }, 30000)

  it('剪贴板读取失败/空文本：走降级粘贴弹窗', async () => {
    const wrapper = await mountEditor()
    Object.defineProperty(navigator, 'clipboard', {
      value: { readText: vi.fn().mockRejectedValue(new Error('denied')) },
      configurable: true
    })
    const pasteButton = wrapper.findAll('button').find((b) => b.text().trim() === '从剪贴板粘贴')!
    await pasteButton.trigger('click')
    await flushPromises()
    expect(wrapper.find('.paste-hint').exists()).toBe(true)
  })
})

describe('TestDataBatchEditor 提交', () => {
  it('客户端校验拦截提交（无效行 + 空表）：不发请求，err-summary 提示 + 行标红 / 空行 toast', async () => {
    // 场景 1：measurement 为空 → required 错误，err-summary + 行标红
    const wrapper = await mountEditor()
    await removeInitialRow(wrapper)
    await pasteRows(wrapper, csvRow('pressure', '', '1.5'))
    await submitButton(wrapper).trigger('click')
    await flushPromises()
    expect(createTestDataBatch).not.toHaveBeenCalled()
    expect(wrapper.find('.err-summary').exists()).toBe(true)
    expect(wrapper.text()).toContain('1 行未通过本地校验')
    expect(wrapper.find('.row-error').exists()).toBe(true)
    expect(wrapper.find('.td-error').exists()).toBe(true)

    // 场景 2：0 行提交 → toast 空行提示，不发请求
    msg.warning.mockClear()
    const wrapper2 = await mountEditor()
    const deleteButtons = wrapper2.findAll('button').filter((b) => b.text().trim() === '删除')
    await deleteButtons[0].trigger('click')
    await flushPromises()
    expect(wrapper2.findAll('tbody tr')).toHaveLength(0)
    await submitButton(wrapper2).trigger('click')
    await flushPromises()
    expect(createTestDataBatch).not.toHaveBeenCalled()
    expect(msg.warning).toHaveBeenCalledWith(expect.stringContaining('请先添加或粘贴数据行'))
  })

  it('后端 422 details：逐行映射到错误标红与 err-summary', async () => {
    const wrapper = await mountEditor()
    await removeInitialRow(wrapper)
    await pasteRows(wrapper, [csvRow('pressure', '入口压强', '101325'), csvRow('voltage', 'rf_voltage', '12.5')].join('\n'))
    const apiError = Object.assign(new Error('校验失败'), {
      status: 422,
      requestId: 'req_422_1',
      details: { errors: [{ index: 1, field: 'value', code: 'validation', message: '数值超出范围' }] }
    })
    vi.mocked(createTestDataBatch).mockRejectedValue(apiError)
    await submitButton(wrapper).trigger('click')
    await flushPromises()
    expect(createTestDataBatch).toHaveBeenCalledWith('proj_01', expect.any(Array))
    expect(wrapper.text()).toContain('1 行校验失败')
    expect(wrapper.text()).toContain('数值超出范围')
  })

  it('提交成功：emit submitted、toast 成功、表格重置为 1 行空白行', async () => {
    const wrapper = await mountEditor()
    await removeInitialRow(wrapper)
    await pasteRows(wrapper, csvRow('pressure', '入口压强', '101325'))
    vi.mocked(createTestDataBatch).mockResolvedValue({ count: 1, items: [] })
    await submitButton(wrapper).trigger('click')
    await flushPromises()
    expect(createTestDataBatch).toHaveBeenCalledWith(
      'proj_01',
      expect.arrayContaining([expect.objectContaining({ data_type: 'pressure', measurement: '入口压强', value: 101325 })])
    )
    expect(wrapper.emitted('submitted')).toHaveLength(1)
    expect(msg.success).toHaveBeenCalledWith(expect.stringContaining('成功录入 1 条'))
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })
})
