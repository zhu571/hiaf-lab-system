import { defineComponent } from 'vue'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BriefPanel from '@/components/business/dashboard/BriefPanel.vue'
import MemberReportPanel from '@/components/business/dashboard/MemberReportPanel.vue'
import { createTestI18n } from '@/test-utils/setup'
import { listReports } from '@/api/logs'
import type { DailyReport } from '@/api/logs'
import { resetDashboardReports, useDashboardReports } from '@/components/business/dashboard/useDashboardReports'
import { createPinia, setActivePinia } from 'pinia'

// BriefPanel / MemberReportPanel 组件测试（R6 §7.1 拆分）：
// 两块面板共用 useDashboardReports 单例——单次取数、selectedDate 双向同步；
// 每个用例经 reset() + 卸载复位挂载计数，保证访问级重新取数语义。

vi.mock('@/api/logs', () => ({
	listReports: vi.fn(),
	listTeamReports: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

const mockedListReports = vi.mocked(listReports)

const Harness = defineComponent({
  setup: useDashboardReports,
  template: '<div><button class="change-date" @click="selectDate(\'2000-01-01\')" /><span class="date">{{ selectedDate }}</span><span class="data">{{ reportsData?.[0]?.summary }}</span><span class="label">{{ briefDayLabel(selectedDate) }}</span></div>'
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function localDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function makeReport(id: string, author: string, summary: string, date: string): DailyReport {
  return {
    id,
    author_id: 'user_' + id,
    author_name: author,
    summary,
    report_date: date,
    raw_text: '',
    content_status: 'final',
    quality_status: 'ok'
  }
}

const yesterday = localDate(new Date(Date.now() - 86400000))

let wrappers: Array<ReturnType<typeof mount>> = []

function mountBoth() {
	const pinia = createPinia()
	setActivePinia(pinia)
  wrappers = [
		mount(BriefPanel, { global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true } } }),
		mount(MemberReportPanel, { global: { plugins: [createTestI18n(), pinia], stubs: { ElDatePicker: true, teleport: true } } })
  ]
  return wrappers
}

describe('useDashboardReports 单例', () => {
  beforeEach(() => {
		setActivePinia(createPinia())
    mockedListReports.mockClear()
    resetDashboardReports()
  })

  afterEach(() => {
    wrappers.forEach((w) => w.unmount())
    wrappers = []
    vi.useRealTimers()
  })

  it('两面板同访仅取数一次（单例共享）', async () => {
    mockedListReports.mockResolvedValue({ items: [], total: 0, page: 1 })
    mountBoth()
    await flushPromises()
    expect(mockedListReports).toHaveBeenCalledTimes(1)
  })

  it('简报渲染 7 天卡片，点击切换选中日期同步成员面板', async () => {
    mockedListReports.mockResolvedValue({ items: [makeReport('r1', '张三', '今日束流稳定', yesterday)], total: 1, page: 1 })
    mountBoth()
    await flushPromises()

    const brief = wrappers[0]
    const member = wrappers[1]
    expect(brief.findAll('.brief-card')).toHaveLength(7)
    expect(brief.text()).toContain('昨天')
    // 成员面板默认显示昨天 → 有 1 篇日报
    expect(member.findAll('.member-card')).toHaveLength(1)

    // 点击简报里「今天」卡片 → selectedDate 同步切换 → 成员面板该日无日报 → 空态
    await brief.findAll('.brief-card')[0].trigger('click')
    expect(brief.findAll('.brief-card')[0].classes()).toContain('active')
    await flushPromises()
    expect(member.find('.el-empty').exists()).toBe(true)
  })

  it('在途请求卸载后不得回写', async () => {
    const pending = deferred<Awaited<ReturnType<typeof listReports>>>()
    mockedListReports.mockReturnValue(pending.promise)
    const wrapper = mount(Harness, { global: { plugins: [createTestI18n()] } })
    wrapper.unmount()

    pending.resolve({ items: [makeReport('stale', '张三', '旧数据', yesterday)], total: 1, page: 1 })
    await flushPromises()
    mockedListReports.mockReturnValue(new Promise(() => {}))
    wrappers = [mount(Harness, { global: { plugins: [createTestI18n()] } })]
    expect(wrappers[0].find('.data').text()).toBe('')
  })

  it('全部卸载后重入不显示旧数据', async () => {
    mockedListReports.mockResolvedValueOnce({ items: [makeReport('r1', '张三', '旧数据', yesterday)], total: 1, page: 1 })
    const wrapper = mount(Harness, { global: { plugins: [createTestI18n()] } })
    await flushPromises()
    expect(wrapper.find('.data').text()).toBe('旧数据')
    wrapper.unmount()

    mockedListReports.mockReturnValue(new Promise(() => {}))
    wrappers = [mount(Harness, { global: { plugins: [createTestI18n()] } })]
    expect(wrappers[0].find('.data').text()).toBe('')
  })

  it('全部卸载后日期恢复为动态计算的昨天', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2030, 0, 3, 12))
    mockedListReports.mockResolvedValue({ items: [], total: 0, page: 1 })
    const wrapper = mount(Harness, { global: { plugins: [createTestI18n()] } })
    await wrapper.find('.change-date').trigger('click')
    wrapper.unmount()

    wrappers = [mount(Harness, { global: { plugins: [createTestI18n()] } })]
    expect(wrappers[0].find('.date').text()).toBe('2030-01-02')
    expect(wrappers[0].find('.label').text()).toBe('昨天')
  })
})
