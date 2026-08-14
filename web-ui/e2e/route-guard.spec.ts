import { expect, test } from '@playwright/test'
import { VIEWER, login } from './helpers'

// 用例 11（J11）：路由守卫真实栈旅程 —— 未登录访问受保护路由跳 /login；
// viewer 越权访问 admin/reviewer 路由跳 /projects（router/guard.ts 规则 1/2/3）。
// 单元层已覆盖守卫全规则矩阵（router/__tests__/guard.test.ts），E2E 只保留此 1 条真实栈验证。
test.describe('路由守卫真实栈', () => {
  test('未登录重定向 /login；viewer 越权访问 /admin/users 与 /agent-candidates 均跳 /projects', async ({ page }) => {
    // 规则 1：未登录访问 /todos → /login
    await page.goto('/todos')
    await page.waitForURL((url) => url.pathname === '/login')
    await expect(page.getByRole('button', { name: '登录' })).toBeVisible()

    // viewer 登录（zhangsan，种子账号）
    await login(page, VIEWER.username, VIEWER.password)

    // 规则 2：meta.admin 路由越权 → /projects
    await page.goto('/admin/users')
    await page.waitForURL((url) => url.pathname === '/projects')
    await expect(page.locator('.project-sidebar')).toBeVisible()

    // 规则 3：meta.reviewer 路由越权（viewer 非 maintainer/admin）→ /projects
    await page.goto('/agent-candidates')
    await page.waitForURL((url) => url.pathname === '/projects')
    await expect(page.locator('.project-sidebar')).toBeVisible()
  })
})
