import { expect, test } from '@playwright/test'
import { ADMIN, login, unique } from './helpers'

// 用例 2：项目 CRUD —— 创建项目（唯一 code）→ 侧边栏列表可见 → 用例数据由 E2E 专属库（lab_e2e）
// 在脚本每次运行前重建 schema 兜底清理（后端无项目删除 API，属已知边界，见 scripts/test-e2e.sh 头注释）。
test.describe('项目 CRUD', () => {
  test('创建项目 → 列表可见', async ({ page }) => {
    const code = unique('e2e')
    await login(page, ADMIN.username, ADMIN.password)

    await page.goto('/projects')
    await page.getByRole('button', { name: '新建项目' }).click()

    const dialog = page.locator('.el-dialog')
    await expect(dialog).toBeVisible()
    await dialog.locator('.el-form-item').filter({ hasText: '编号' }).locator('input').fill(code)
    await dialog.locator('.el-form-item').filter({ hasText: '名称' }).locator('input').fill(`${code} 名称`)
    await dialog.locator('.el-form-item').filter({ hasText: '简称' }).locator('input').fill(code)
    await dialog.getByRole('button', { name: '保存' }).click()

    // 侧边栏出现新项目（code 唯一，直接按 code 文本断言）
    const item = page.locator('.project-item', { hasText: code })
    await expect(item).toBeVisible()
    await expect(item.locator('strong')).toHaveText(code)
  })
})
