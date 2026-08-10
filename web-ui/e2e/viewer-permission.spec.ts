import { expect, test } from '@playwright/test'
import { VIEWER, login } from './helpers'

// 用例 8：viewer 权限 —— zhangsan(viewer) 登录后项目页/批次页无任何新建/编辑入口
// （前端按钮隐藏只是 UX，后端强校验不变；本用例按实际前端行为断言「无编辑入口」）。
// zhangsan 是种子项目 GAS-TARGET（migrations/009）的 viewer 成员，列表必可见。
test.describe('viewer 权限', () => {
  test('viewer 看不到新建项目/新建批次/AI 生成入口', async ({ page }) => {
    await login(page, VIEWER.username, VIEWER.password)

    // 项目列表：无「新建项目」按钮
    await page.goto('/projects')
    await expect(page.locator('.project-sidebar')).toBeVisible()
    await expect(page.getByRole('button', { name: '新建项目' })).toHaveCount(0)

    // 进入种子项目 GAS-TARGET
    const item = page.locator('.project-item', { hasText: 'GAS-TARGET' })
    await expect(item).toBeVisible()
    await item.click()
    await expect(page).toHaveURL(/\/projects\/[0-9a-f-]{36}/)
    // 项目 id 取 URL 末段（项目 id 为 UUID，不含斜杠；.pop 与项目 ES2020 lib 一致）
    const projectId = page.url().split('/').pop()

    // 批次 tab：无「新建批次」「AI 生成步骤」按钮
    await page.getByRole('tab', { name: '批次' }).click()
    await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/experiment-runs`))
    await expect(page.getByRole('button', { name: '新建批次' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'AI 生成步骤' })).toHaveCount(0)

    // 测试数据 tab：viewer 无「录入」tab，默认落在数据列表
    await page.getByRole('tab', { name: '数据' }).click()
    await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/test-data`))
    await expect(page.getByRole('tab', { name: '录入' })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: '数据列表' })).toBeVisible()
  })
})
