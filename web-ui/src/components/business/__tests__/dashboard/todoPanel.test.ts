import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TodoPanel from '@/components/business/dashboard/TodoPanel.vue'
import { createTestI18n } from '@/test-utils/setup'
import { createTodo, doneTodo, listTodos, type Todo } from '@/api/todos'

// TodoPanel 组件测试（R6 §7.1 拆分，方案附录 D：dashboard 四块「组件测试断言」）：
// 待办渲染/空态/添加/完成，逻辑从 DashboardView 等价平移后口径不变。

vi.mock('@/api/todos', () => ({
  listTodos: vi.fn(),
  createTodo: vi.fn(),
  doneTodo: vi.fn(),
  deferTodo: vi.fn(),
  llmParse: vi.fn(),
  llmAdd: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

const mockedListTodos = vi.mocked(listTodos)
const mockedCreateTodo = vi.mocked(createTodo)
const mockedDoneTodo = vi.mocked(doneTodo)

function makeTodo(id: string, title: string, status: Todo['status'] = 'pending'): Todo {
  return {
    id,
    title,
    priority: 'high',
    status,
    source: 'manual',
    created_by: 'user_01',
    created_for: 'user_01',
    created_at: '2026-08-17T08:00:00+08:00',
    updated_at: '2026-08-17T08:00:00+08:00',
    owner_display_name: '测试员'
  }
}

async function mountPanel() {
  const wrapper = mount(TodoPanel, {
    global: { plugins: [createTestI18n()], stubs: { teleport: true } }
  })
  await flushPromises()
  return wrapper
}

// 文件内 vi.mock 工厂共享同一 mock，逐用例清空调用记录避免跨用例计数漂移
beforeEach(() => {
  mockedListTodos.mockClear()
  mockedCreateTodo.mockClear()
  mockedDoneTodo.mockClear()
})

describe('TodoPanel', () => {
  it('渲染待办行（优先级/标题/来源）', async () => {
    mockedListTodos.mockResolvedValueOnce([makeTodo('todo_1', '检查真空计'), makeTodo('todo_2', '校准束流')])
    const wrapper = await mountPanel()
    const rows = wrapper.findAll('.todo-row')
    expect(rows).toHaveLength(2)
    expect(wrapper.text()).toContain('检查真空计')
    expect(wrapper.text()).toContain('校准束流')
    expect(wrapper.text()).toContain('手动')
  })

  it('空态显示 el-empty，添加入口保留', async () => {
    mockedListTodos.mockResolvedValueOnce([])
    const wrapper = await mountPanel()
    expect(wrapper.find('.el-empty').exists()).toBe(true)
    expect(wrapper.findAll('.todo-add-row')).toHaveLength(2)
  })

  it('输入标题添加待办：createTodo 带 title，成功后刷新列表', async () => {
    mockedListTodos.mockResolvedValueOnce([])
    mockedCreateTodo.mockResolvedValueOnce({} as never)
    const wrapper = await mountPanel()
    const rows = wrapper.findAll('.todo-add-row')
    await rows[0].find('input').setValue('新待办')
    await rows[0].find('.el-button').trigger('click')
    expect(mockedCreateTodo).toHaveBeenCalledWith({ title: '新待办' })
    expect(mockedListTodos).toHaveBeenCalledTimes(2)
  })

  it('勾选待办调用 doneTodo', async () => {
    mockedListTodos.mockResolvedValueOnce([makeTodo('todo_1', '检查真空计')])
    const wrapper = await mountPanel()
    await wrapper.find('.todo-row input[type="checkbox"]').setValue(true)
    expect(mockedDoneTodo).toHaveBeenCalledWith('todo_1')
  })
})