import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useProjectStore } from '../project'
import { makeProject } from '../../test-utils/factories'

// mock api 层：listProjects 打桩，不发起真实网络请求
const mocks = vi.hoisted(() => ({
  listProjects: vi.fn()
}))

vi.mock('../../api/projects', () => ({
  listProjects: mocks.listProjects
}))

describe('project store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('load：填充项目列表并默认选中第一个', async () => {
    const projects = [makeProject({ id: 'p1' }), makeProject({ id: 'p2' })]
    mocks.listProjects.mockResolvedValue(projects)

    const store = useProjectStore()
    await store.load()

    expect(mocks.listProjects).toHaveBeenCalled()
    expect(store.projects).toEqual(projects)
    expect(store.currentId).toBe('p1')
  })

  it('load：currentId 已选中时不被首个项目覆盖', async () => {
    const projects = [makeProject({ id: 'p1' }), makeProject({ id: 'p2' })]
    mocks.listProjects.mockResolvedValue(projects)

    const store = useProjectStore()
    store.currentId = 'p2'
    await store.load()

    expect(store.currentId).toBe('p2')
  })

  it('load：空列表时 projects 为空且 currentId 不变', async () => {
    mocks.listProjects.mockResolvedValue([])

    const store = useProjectStore()
    await store.load()

    expect(store.projects).toEqual([])
    expect(store.currentId).toBe('')
  })

  it('load：api 失败时异常向上抛，状态保持初始', async () => {
    mocks.listProjects.mockRejectedValue(new Error('network'))

    const store = useProjectStore()
    await expect(store.load()).rejects.toThrow('network')
    expect(store.projects).toEqual([])
    expect(store.currentId).toBe('')
  })

  it('select：切换当前选中项目', () => {
    const store = useProjectStore()
    store.projects = [makeProject({ id: 'p1' }), makeProject({ id: 'p2' })]
    store.select('p2')
    expect(store.currentId).toBe('p2')
  })

  it('current getter：优先按 currentId 匹配，未匹配时回退到第一个', () => {
    const store = useProjectStore()
    const projects = [makeProject({ id: 'p1' }), makeProject({ id: 'p2' })]
    store.projects = projects

    store.currentId = 'p2'
    expect(store.current?.id).toBe('p2')

    store.currentId = 'nonexistent'
    expect(store.current?.id).toBe('p1')

    store.projects = []
    expect(store.current).toBeUndefined()
  })
})

// §3.3 store 边界补充（5 例预算：auth 3 + project 2）
describe('project store 边界', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('load 重复调用幂等性：currentId 已选中时，第二次 load 返回新列表不覆盖选中', async () => {
    mocks.listProjects.mockResolvedValueOnce([makeProject({ id: 'p1' }), makeProject({ id: 'p2' })])

    const store = useProjectStore()
    await store.load()
    expect(store.currentId).toBe('p1')

    const newList = [makeProject({ id: 'p9' })]
    mocks.listProjects.mockResolvedValueOnce(newList)
    await store.load()

    expect(store.projects).toEqual(newList)
    expect(store.currentId).toBe('p1')
    // 新列表已不含 p1：current getter 按规则回退到列表首个
    expect(store.current?.id).toBe('p9')
  })

  it('select 不存在的 id：currentId 被写入，current getter 回退到第一个项目', () => {
    const store = useProjectStore()
    store.projects = [makeProject({ id: 'p1' }), makeProject({ id: 'p2' })]

    store.select('ghost-id')
    expect(store.currentId).toBe('ghost-id')
    expect(store.current?.id).toBe('p1')
  })
})
