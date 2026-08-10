import { expect, test } from '@playwright/test'
import { ADMIN, api, login } from './helpers'

// 用例 7：i18n 切换 —— 设置页切英文 → 主导航变英文 → 切回中文（语言偏好同时回写后端 users.language）
// 语言偏好持久化在后端 users.language（登录时会覆盖回显），若本用例中途失败会残留英文，
// 污染后续用例的断言（中文文案）——afterEach 里用 API 强制恢复中文兜底。
test.describe('i18n 切换', () => {
  test('设置页 中文 → English → 中文', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)

    await page.goto('/settings')
    const nav = page.locator('.nav')
    await expect(nav).toContainText('项目')
    await expect(page.getByRole('heading', { name: '个人设置' })).toBeVisible()

    // 切英文（role=option 只命中可见下拉项，避开隐藏副本）
    await page.locator('.language-row .el-select').click()
    await page.getByRole('option', { name: 'English' }).click()
    await expect(nav).toContainText('Projects', { timeout: 15_000 })
    await expect(nav).not.toContainText('项目')
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()

    // 切回中文
    await page.locator('.language-row .el-select').click()
    await page.getByRole('option', { name: '中文' }).click()
    await expect(nav).toContainText('项目', { timeout: 15_000 })
    await expect(page.getByRole('heading', { name: '个人设置' })).toBeVisible()
  })

  test.afterEach(async ({ page }) => {
    // 兜底：无论用例成败都恢复后端语言偏好为 zh，避免污染后续用例
    await api(page, 'PATCH', '/api/v1/auth/profile', { language: 'zh' }).catch(() => undefined)
  })
})
