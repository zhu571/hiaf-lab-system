import { expect, test } from '@playwright/test'
import { ADMIN, api, createProjectViaApi, createRunViaApi, login, unique } from './helpers'

// 用例 6：AI 助手入口可见性 —— 详情页「AI 生成步骤」按钮存在且可用（防「功能消失」回归）。
// 只验证入口可见，不点击发送（不依赖真实 LLM / 不发 AI 请求）。
test.describe('AI 助手入口可见性', () => {
  let runId = ''

  test('批次详情页「AI 生成步骤」按钮可见且可用', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)

    const project = await createProjectViaApi(page, unique('e2e'))
    const run = await createRunViaApi(page, project.id, unique('e2e-run'))
    runId = run.id

    await page.goto(`/experiment-runs/${run.id}`)
    await expect(page.locator('.head-row')).toContainText(run.name)

    // AI 生成步骤按钮存在且可用（不点击，避免真实 LLM 请求）
    const aiButton = page.locator('.head-row').getByRole('button', { name: 'AI 生成步骤' })
    await expect(aiButton).toBeVisible()
    await expect(aiButton).toBeEnabled()

    // 「步骤」tab 存在并可切换（AI 生成结果落点的上下文）
    await page.getByRole('tab', { name: '步骤' }).click()
    await expect(page.getByText('实验步骤')).toBeVisible()
  })

  test.afterEach(async ({ page }) => {
    if (runId) {
      await api(page, 'DELETE', `/api/v1/experiment-runs/${runId}`).catch(() => undefined)
    }
    runId = ''
  })
})
