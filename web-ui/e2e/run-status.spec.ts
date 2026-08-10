import { expect, test } from '@playwright/test'
import { ADMIN, api, createProjectViaApi, login, unique } from './helpers'

// 用例 4：批次状态流转 —— 新建 experiment run → planned → start → active → complete → completed
test.describe('批次状态流转', () => {
  let projectId = ''
  let runId = ''

  test('新建批次 → 状态 planned → 进行中 → 已完成', async ({ page }) => {
    const runName = unique('e2e-run')
    await login(page, ADMIN.username, ADMIN.password)

    // 准备：项目创建走 API（真实后端），UI 流程聚焦批次流转本身
    const project = await createProjectViaApi(page, unique('e2e'))
    projectId = project.id

    // 进入项目 → 批次 tab → 新建批次
    await page.goto(`/projects/${projectId}`)
    await page.getByRole('tab', { name: '批次' }).click()
    await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/experiment-runs`))
    await page.getByRole('button', { name: '新建批次' }).click()

    const dialog = page.locator('.el-dialog')
    await dialog.locator('.el-form-item').filter({ hasText: '名称（必填）' }).locator('input').fill(runName)
    await dialog.getByRole('button', { name: '保存' }).click()

    // 列表出现批次卡片（初始 planned）→ 点进详情
    const card = page.locator('.run-card', { hasText: runName })
    await expect(card).toBeVisible()
    await expect(card).toContainText('planned')
    await card.click()
    await expect(page).toHaveURL(/\/experiment-runs\//)

    // planned → start → active
    const head = page.locator('.head-row')
    await expect(head).toContainText('planned')
    await head.getByRole('button', { name: '开始' }).click()
    await expect(head).toContainText('active', { timeout: 15_000 })

    // active → complete（确认框）→ completed
    await head.getByRole('button', { name: '完成' }).click()
    await page.getByRole('button', { name: '确认', exact: true }).click()
    await expect(head).toContainText('completed', { timeout: 15_000 })

    // 清理：API 删除批次（真实后端，软删除）
    runId = page.url().split('/').pop() as string
    await api(page, 'DELETE', `/api/v1/experiment-runs/${runId}`)
  })

  test.afterEach(async ({ page }) => {
    // 兜底：用例失败/中途退出时也清理批次与项目（项目无删除 API，随脚本重建库清理）
    if (runId) {
      await api(page, 'DELETE', `/api/v1/experiment-runs/${runId}`).catch(() => undefined)
    }
    runId = ''
    projectId = ''
  })
})
