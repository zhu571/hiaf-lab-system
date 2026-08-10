import { expect, type Page } from '@playwright/test'

// E2E 共享工具：登录、API 直调（真实后端）、唯一前缀。
// 种子用户来自 migrations/009_test_data.up.sql：haofan(admin)/zhangsan(viewer)，密码 Test1234!。

export const ADMIN = { username: 'haofan', password: 'Test1234!' }
export const VIEWER = { username: 'zhangsan', password: 'Test1234!' }

// 用例内唯一前缀：避免与开发数据冲突、支持失败后重跑
export function unique(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`
}

// 通过登录页登录（真实 UI 流程），登录成功会 router.push('/')。
export async function login(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.locator('input[autocomplete="username"]').fill(username)
  await page.locator('input[autocomplete="current-password"]').fill(password)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL((url) => url.pathname === '/')
  await expect(page.locator('.nav')).toBeVisible()
}

// 退出登录（用户卡片下拉 → 退出登录）
export async function logout(page: Page): Promise<void> {
  await page.locator('.user-card-btn').click()
  await page.getByRole('menuitem', { name: '退出登录' }).click()
  await page.waitForURL((url) => url.pathname === '/login')
}

// 以当前页面会话（共享 cookie jar）直调真实后端 API。
// 写请求自动补 X-CSRF-Token（读 csrf_token cookie）与 Idempotency-Key。
export async function api<T>(page: Page, method: string, url: string, data?: unknown): Promise<T> {
  const csrf = await page.evaluate(() => {
    const m = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/)
    return m ? decodeURIComponent(m[1]) : ''
  })
  const headers: Record<string, string> = {}
  if (csrf) headers['X-CSRF-Token'] = csrf
  if (method !== 'GET') headers['Idempotency-Key'] = crypto.randomUUID()
  // data 直接传对象：Playwright 会序列化并设 Content-Type: application/json
  // （传 JSON.stringify 字符串会变成 text/plain，虽不影响 Go 的 json.Decoder，但非惯例）
  const res = await page.request.fetch(url, {
    method,
    headers,
    data
  })
  const body = await res.json().catch(() => null)
  if (!res.ok()) {
    throw new Error(`${method} ${url} -> ${res.status()}: ${JSON.stringify(body?.error || body)}`)
  }
  return body.data as T
}

export type Project = { id: string; code: string; name: string }
export type Run = { id: string; name: string; status: string }

// 以 admin 会话建项目（code/name 必填，code 唯一）→ 返回 { id, code, name }
export async function createProjectViaApi(page: Page, code: string): Promise<Project> {
  return api<Project>(page, 'POST', '/api/v1/projects', {
    code,
    name: `${code} 名称`,
    short_name: code,
    description: 'E2E 冒烟测试项目'
  })
}

// 以 admin 会话建批次（name 必填）→ 返回 { id, name, status }
export async function createRunViaApi(page: Page, projectId: string, name: string): Promise<Run> {
  return api<Run>(page, 'POST', `/api/v1/projects/${projectId}/experiment-runs`, {
    name,
    run_type: 'test',
    gas_type: 'He'
  })
}

// 服务器时区为 Asia/Shanghai，按该时区取今天的 YYYY-MM-DD（历史列表断言用）
export function todayInShanghai(): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).format(new Date())
}
