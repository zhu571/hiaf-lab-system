import { describe, it, expect, vi, beforeEach } from 'vitest'
import * as agentApi from '../agent'
import * as alertsApi from '../alerts'
import * as askApi from '../ask'
import * as assemblyApi from '../assembly'
import * as attachmentsApi from '../attachments'
import * as auditApi from '../audit'
import * as authApi from '../auth'
import * as experiencesApi from '../experiences'
import * as instrumentsApi from '../instruments'
import * as issuesApi from '../issues'
import * as logsApi from '../logs'
import * as projectsApi from '../projects'
import * as rfmatchApi from '../rfmatch'
import * as runsApi from '../runs'
import * as sensorsApi from '../sensors'
import * as stepTemplatesApi from '../stepTemplates'
import * as systemApi from '../system'
import * as testdataApi from '../testdata'
import * as todosApi from '../todos'

// 19 个业务 api 模块契约冒烟（方案 §3.4-2）：vi.mock './client' 的 request/requestWithMeta，
// 逐函数断言 url/method/params/data 拼装（不发起真实网络）。
const mocks = vi.hoisted(() => ({
  request: vi.fn(),
  requestWithMeta: vi.fn(),
  setCSRFToken: vi.fn(),
  newIdempotencyKey: vi.fn(),
  api: {}
}))

vi.mock('../client', () => mocks)

function lastRequestConfig() {
  expect(mocks.request).toHaveBeenCalled()
  return mocks.request.mock.calls[mocks.request.mock.calls.length - 1][0]
}

beforeEach(() => {
  mocks.request.mockReset().mockResolvedValue({})
  mocks.requestWithMeta.mockReset().mockResolvedValue({})
  mocks.setCSRFToken.mockReset()
})

describe('agent.ts', () => {
  it('listAgentCandidates：GET 集合 + params 原样透传', async () => {
    await agentApi.listAgentCandidates({ page: 2, per_page: 10 })
    expect(lastRequestConfig()).toEqual({ url: '/agent/candidates', params: { page: 2, per_page: 10 } })
  })

  it('approveCandidate：POST /agent/candidates/{id}/approve', async () => {
    await agentApi.approveCandidate('cand-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/agent/candidates/cand-1/approve', method: 'POST' })
  })
})

describe('alerts.ts', () => {
  it('resolveAlert：POST /alerts/resolve 携带 body', async () => {
    await alertsApi.resolveAlert({ id: 'a-1', title: '告警标题' })
    expect(lastRequestConfig()).toEqual({ url: '/alerts/resolve', method: 'POST', data: { id: 'a-1', title: '告警标题' } })
  })
})

describe('ask.ts', () => {
  it('askChat：显式 Idempotency-Key header 优先，data 携带 question', async () => {
    await askApi.askChat('最近的测试数据', 'manual-key')
    expect(lastRequestConfig()).toMatchObject({
      url: '/ask/chat',
      method: 'POST',
      data: { question: '最近的测试数据' },
      headers: { 'Idempotency-Key': 'manual-key' }
    })
  })

  it('askHistory：分页参数透传，url 为 /ask/history', async () => {
    await askApi.askHistory({ page: 3, per_page: 20 })
    expect(lastRequestConfig()).toEqual({ url: '/ask/history', params: { page: 3, per_page: 20 } })
  })
})

describe('assembly.ts', () => {
  it('createAssemblyStep：POST /projects/{pid}/assembly 携带 data', async () => {
    await assemblyApi.createAssemblyStep('p-1', { name: '步骤A', step_order: 1 })
    expect(lastRequestConfig()).toMatchObject({ url: '/projects/p-1/assembly', method: 'POST', data: { name: '步骤A', step_order: 1 } })
  })

  it('transitionAssemblyStep：无 override_reason 时 data 仅含 transition，有则追加 override_reason', async () => {
    await assemblyApi.transitionAssemblyStep('s-1', 'complete')
    expect(lastRequestConfig()).toMatchObject({ url: '/assembly/s-1', method: 'PATCH', data: { transition: 'complete' } })

    await assemblyApi.transitionAssemblyStep('s-1', 'complete', '跳过前置校验')
    expect(lastRequestConfig()).toMatchObject({ url: '/assembly/s-1', method: 'PATCH', data: { transition: 'complete', override_reason: '跳过前置校验' } })
  })
})

describe('attachments.ts', () => {
  it('uploadAttachment：multipart FormData 拼接 file/entity_type/entity_id/description；无 entity 时不附带', async () => {
    const file = new File(['x'], 'a.txt', { type: 'text/plain' })
    await attachmentsApi.uploadAttachment(file, 'issue', 'i-1', '截图')
    const form = lastRequestConfig().data as FormData
    expect(form.get('file')).toBe(file)
    expect(form.get('entity_type')).toBe('issue')
    expect(form.get('entity_id')).toBe('i-1')
    expect(form.get('description')).toBe('截图')

    await attachmentsApi.uploadAttachment(file)
    const form2 = lastRequestConfig().data as FormData
    expect(form2.get('entity_type')).toBeNull()
    expect(form2.get('entity_id')).toBeNull()
  })
})

describe('audit.ts', () => {
  it('listAuditEvents：params 透传（C12 审计事件列表）', async () => {
    await auditApi.listAuditEvents({ action: 'login', user_id: 'u-1', page: 1 })
    expect(lastRequestConfig()).toEqual({ url: '/audit/events', params: { action: 'login', user_id: 'u-1', page: 1 } })
  })
})

describe('auth.ts', () => {
  it('login：POST /auth/login，成功后 setCSRFToken 联动（csrf_token 更新）', async () => {
    mocks.request.mockResolvedValue({ csrf_token: 'csrf-1', must_change_password: false, user: { id: 'u-1' } })
    const data = await authApi.login('alice', 'secret')
    expect(lastRequestConfig()).toEqual({ url: '/auth/login', method: 'POST', data: { username: 'alice', password: 'secret' } })
    expect(mocks.setCSRFToken).toHaveBeenCalledWith('csrf-1')
    expect(data.csrf_token).toBe('csrf-1')
  })

  it('refresh：POST /auth/refresh，成功后 setCSRFToken 联动（单飞刷新同步 CSRF）', async () => {
    mocks.request.mockResolvedValue({ csrf_token: 'csrf-2', must_change_password: false, user: { id: 'u-1' } })
    await authApi.refresh()
    expect(lastRequestConfig()).toEqual({ url: '/auth/refresh', method: 'POST', data: {} })
    expect(mocks.setCSRFToken).toHaveBeenCalledWith('csrf-2')
  })
})

describe('experiences.ts', () => {
  it('createExperience：POST /experiences 携带 data', async () => {
    await experiencesApi.createExperience({ title: '经验标题', content: '正文' })
    expect(lastRequestConfig()).toMatchObject({ url: '/experiences', method: 'POST', data: { title: '经验标题', content: '正文' } })
  })
})

describe('instruments.ts', () => {
  it('listInstruments：GET 集合无参数', async () => {
    await instrumentsApi.listInstruments()
    expect(lastRequestConfig()).toEqual({ url: '/instruments' })
  })

  it('executeCommand：POST /instruments/{id}/commands 携带 {command, params}', async () => {
    await instrumentsApi.executeCommand('ins-1', 'SET:FREQ', { freq: 100 })
    expect(lastRequestConfig()).toMatchObject({ url: '/instruments/ins-1/commands', method: 'POST', data: { command: 'SET:FREQ', params: { freq: 100 } } })
  })

  it('emergencyStop：POST /instruments/{id}/emergency-stop（急停写路径）', async () => {
    await instrumentsApi.emergencyStop('ins-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/instruments/ins-1/emergency-stop', method: 'POST' })
  })

  it('piezoStart/piezoStop：POST 控制端点无 data', async () => {
    await instrumentsApi.piezoStart()
    expect(lastRequestConfig()).toMatchObject({ url: '/instruments/piezo/start', method: 'POST' })
    await instrumentsApi.piezoStop()
    expect(lastRequestConfig()).toMatchObject({ url: '/instruments/piezo/stop', method: 'POST' })
  })

  it('interpretCommand：history 截断为最近 10 条（requestWithMeta）', async () => {
    const history = Array.from({ length: 12 }, (_, i) => ({ role: 'user' as const, content: `消息${i}` }))
    await instrumentsApi.interpretCommand('ins-1', '输入', history)
    expect(mocks.requestWithMeta).toHaveBeenCalled()
    const cfg = mocks.requestWithMeta.mock.calls[mocks.requestWithMeta.mock.calls.length - 1][0]
    expect(cfg.url).toBe('/instruments/ins-1/nl-commands')
    const data = cfg.data as { input: string; history: { content: string }[] }
    expect(data.input).toBe('输入')
    expect(data.history).toHaveLength(10)
    expect(data.history[9].content).toBe('消息11')
  })
})

describe('issues.ts', () => {
  it('transitionIssue：reason 非空时 add_comment=true（追加评论语义）', async () => {
    await issuesApi.transitionIssue('i-1', 'closed')
    expect(lastRequestConfig()).toMatchObject({ url: '/issues/i-1/transition', method: 'POST', data: { target_status: 'closed', reason: '', add_comment: false } })

    await issuesApi.transitionIssue('i-1', 'closed', '已解决')
    expect(lastRequestConfig()).toMatchObject({ url: '/issues/i-1/transition', method: 'POST', data: { target_status: 'closed', reason: '已解决', add_comment: true } })
  })
})

describe('logs.ts', () => {
  it('todayReport：POST /daily-reports/today 携带空对象', async () => {
    await logsApi.todayReport()
    expect(lastRequestConfig()).toEqual({ url: '/daily-reports/today', method: 'POST', data: {} })
  })

  it('submitReport：POST 携带 force，缺省 false、传 true 透传', async () => {
    await logsApi.submitReport('r-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/daily-reports/r-1/submit', method: 'POST', data: { force: false } })
    await logsApi.submitReport('r-1', true)
    expect(lastRequestConfig()).toMatchObject({ url: '/daily-reports/r-1/submit', method: 'POST', data: { force: true } })
  })

  it('reportByDate：GET /daily-reports/by-date 携带 date param', async () => {
    await logsApi.reportByDate('2026-08-01')
    expect(lastRequestConfig()).toEqual({ url: '/daily-reports/by-date', params: { date: '2026-08-01' } })
  })
})

describe('projects.ts', () => {
  it('listProjects：status 过滤参数透传', async () => {
    await projectsApi.listProjects('active')
    expect(lastRequestConfig()).toEqual({ url: '/projects', params: { status: 'active' } })
  })
})

describe('rfmatch.ts', () => {
  it('deleteRFMatching：无 reason 时 data 为 undefined，带 reason 时透传', async () => {
    await rfmatchApi.deleteRFMatching('r-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/rf-matching/r-1', method: 'DELETE', data: undefined })

    await rfmatchApi.deleteRFMatching('r-1', '重复录入')
    expect(lastRequestConfig()).toMatchObject({ url: '/rf-matching/r-1', method: 'DELETE', data: { reason: '重复录入' } })
  })
})

describe('runs.ts', () => {
  it('createRun：POST /projects/{pid}/experiment-runs 携带 data', async () => {
    await runsApi.createRun('p-1', { name: '批次A' } as never)
    expect(lastRequestConfig()).toMatchObject({ url: '/projects/p-1/experiment-runs', method: 'POST', data: { name: '批次A' } })
  })

  it('transitionRun：PATCH /experiment-runs/{id} 携带 {transition}', async () => {
    await runsApi.transitionRun('run-1', 'start')
    expect(lastRequestConfig()).toMatchObject({ url: '/experiment-runs/run-1', method: 'PATCH', data: { transition: 'start' } })
  })

  it('deleteRun：DELETE /experiment-runs/{id}', async () => {
    await runsApi.deleteRun('run-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/experiment-runs/run-1', method: 'DELETE' })
  })

  it('addReportLink：POST 嵌套 URL 关联日报', async () => {
    await runsApi.addReportLink('run-1', 'rep-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/experiment-runs/run-1/daily-reports/rep-1', method: 'POST' })
  })

  it('reorderRunSteps：POST /run-steps/reorder 携带 {run_id, steps}（requestWithMeta）', async () => {
    await runsApi.reorderRunSteps('run-1', [{ id: 's-1', step_order: 2 }, { id: 's-2', step_order: 1 }])
    expect(mocks.requestWithMeta).toHaveBeenCalled()
    const cfg = mocks.requestWithMeta.mock.calls[mocks.requestWithMeta.mock.calls.length - 1][0]
    expect(cfg).toMatchObject({ url: '/run-steps/reorder', method: 'POST', data: { run_id: 'run-1', steps: [{ id: 's-1', step_order: 2 }, { id: 's-2', step_order: 1 }] } })
  })
})

describe('sensors.ts', () => {
  it('getLatest：tags 逗号拼接；空数组省略 params', async () => {
    await sensorsApi.getLatest(['gascell_t', 'gascell_p'])
    expect(lastRequestConfig()).toMatchObject({ url: '/sensors/latest', params: { tags: 'gascell_t,gascell_p' } })

    await sensorsApi.getLatest()
    expect(lastRequestConfig()).toMatchObject({ url: '/sensors/latest', params: {} })
  })

  it('getHistory：to/interval 非空才加入 params，缺省仅 tag+from', async () => {
    await sensorsApi.getHistory('gascell_t')
    expect(lastRequestConfig()).toMatchObject({ url: '/sensors/history', params: { tag: 'gascell_t', from: '-1h' } })

    await sensorsApi.getHistory('gascell_t', '-30m', '2026-08-01T00:00:00+08:00', '1m')
    expect(lastRequestConfig()).toMatchObject({
      url: '/sensors/history',
      params: { tag: 'gascell_t', from: '-30m', to: '2026-08-01T00:00:00+08:00', interval: '1m' }
    })
  })
})

describe('stepTemplates.ts', () => {
  it('generateSteps：POST /step-templates/generate 携带 {kind, prompt}', async () => {
    await stepTemplatesApi.generateSteps('assembly', '帮我生成装配步骤')
    expect(lastRequestConfig()).toMatchObject({ url: '/step-templates/generate', method: 'POST', data: { kind: 'assembly', prompt: '帮我生成装配步骤' } })
  })
})

describe('system.ts', () => {
  it('getVersion：GET /admin/system/version', async () => {
    await systemApi.getVersion()
    expect(lastRequestConfig()).toMatchObject({ url: '/admin/system/version' })
  })
})

describe('testdata.ts', () => {
  it('createTestDataBatch：POST 批量端点，请求体为数组（≤100 条）', async () => {
    const rows = [
      { data_type: 'pressure', measurement: '入口', value: 100 },
      { data_type: 'temperature', measurement: '靶温', value: 300 }
    ]
    await testdataApi.createTestDataBatch('p-1', rows)
    expect(lastRequestConfig()).toMatchObject({ url: '/projects/p-1/test-data/batch', method: 'POST', data: rows })
  })
})

describe('todos.ts', () => {
  it('listTodos：limit=100 兜底，date 缺省时不传，scope/status 透传', async () => {
    await todosApi.listTodos({ scope: 'mine', status: 'all' })
    const cfg = lastRequestConfig()
    expect(cfg.url).toBe('/todos')
    expect(cfg.params).toMatchObject({ scope: 'mine', status: 'all', limit: 100 })
    expect(cfg.params.date).toBeUndefined()
  })

  it('createTodo：POST /todos 携带 data（project_id 可空）', async () => {
    await todosApi.createTodo({ title: '记录真空度', priority: 'high', project_id: null })
    expect(lastRequestConfig()).toMatchObject({ url: '/todos', method: 'POST', data: { title: '记录真空度', priority: 'high', project_id: null } })
  })

  it('doneTodo：PATCH /todos/{id}/done 携带空对象', async () => {
    await todosApi.doneTodo('t-1')
    expect(lastRequestConfig()).toMatchObject({ url: '/todos/t-1/done', method: 'PATCH', data: {} })
  })
})
