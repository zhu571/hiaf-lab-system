import { expect, test } from '@playwright/test'
import { ADMIN, login } from './helpers'

// 用例 1：登录（admin 凭据）→ 跳转首页仪表盘（方案 §4.4 用例 1）
test.describe('登录', () => {
  test('haofan 用正确凭据登录 → 进入首页并显示主导航', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)
    await expect(page.getByText('实验室仪表盘')).toBeVisible()
    await expect(page.locator('.nav')).toContainText('首页')
    await expect(page.locator('.nav')).toContainText('项目')
  })

  test('错误密码 → 显示登录失败提示且停留在登录页', async ({ page }) => {
    await page.goto('/login')
    await page.locator('input[autocomplete="username"]').fill(ADMIN.username)
    await page.locator('input[autocomplete="current-password"]').fill('wrong-password')
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page.locator('.el-alert')).toContainText('用户名或密码错误')
    await expect(page).toHaveURL(/\/login/)
  })
})
