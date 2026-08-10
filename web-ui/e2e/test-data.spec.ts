import { expect, test } from '@playwright/test'
import { ADMIN, api, createProjectViaApi, login, unique } from './helpers'

// 用例 5：测试数据 —— 录入一条 → 提交 → 数据列表可见 → 清理（标记无效/软删）
test.describe('测试数据', () => {
  let projectId = ''
  let dataId = ''

  test('录入一条测试数据 → 列表可见', async ({ page }) => {
    const measurement = unique('beam_current')
    await login(page, ADMIN.username, ADMIN.password)

    const project = await createProjectViaApi(page, unique('e2e'))
    projectId = project.id

    await page.goto(`/projects/${projectId}/test-data`)

    // 非 viewer 默认落在「录入」tab
    await expect(page.getByRole('tab', { name: '录入' })).toHaveClass(/is-active/)
    const row = page.locator('.batch-editor .el-table__row').first()
    await expect(row).toBeVisible()

    // 填第一行：数据类型（下拉）→ 测量项 → 数值 → 单位
    await row.locator('.el-select').nth(0).click()
    await page.getByRole('option', { name: 'cryo' }).click()
    await row.locator('input[placeholder="如 beam_current"]').fill(measurement)
    await row.locator('.el-input-number input').fill('3.5')
    await row.locator('input[placeholder="如 K / mbar / V"]').fill('mA')

    await page.getByRole('button', { name: '提交（1 条）' }).click()
    await expect(page.locator('.el-message--success')).toContainText('成功录入 1 条')

    // 切「数据列表」tab：新数据可见（含单位）
    await page.getByRole('tab', { name: '数据列表' }).click()
    const listRow = page.locator('.el-table__row', { hasText: measurement })
    await expect(listRow.first()).toBeVisible()
    await expect(listRow.first()).toContainText('3.5 mA')

    // 清理：通过 API 拿 id → 删除（标记 invalid）
    const data = await api<{ items: { id: string }[] }>(page, 'GET', `/api/v1/projects/${projectId}/test-data`)
    const hit = data.items.find((i) => (i as unknown as { measurement: string }).measurement === measurement)
    if (hit) {
      dataId = hit.id
      await api(page, 'DELETE', `/api/v1/test-data/${hit.id}`)
    }
  })

  test.afterEach(async ({ page }) => {
    if (dataId) {
      await api(page, 'DELETE', `/api/v1/test-data/${dataId}`).catch(() => undefined)
    }
    dataId = ''
    projectId = ''
  })
})
