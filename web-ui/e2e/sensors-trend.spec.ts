import { expect, test } from '@playwright/test'
import { ADMIN, login } from './helpers'

// 用例 9（J9）：传感器页降级旅程 —— E2E 环境无 InfluxDB 实例（test-e2e.sh:135
// INFLUXDB_ADDR 指向 127.0.0.1:18086 无服务），getLatest/getHistory 必然 502，
// 页面须呈现确定性错误态而非崩溃；曲线渲染正确性由组件/单测层覆盖。
// 断言锚点：SensorsView.vue 最新读数 StateBlock 错误（:35-36）与历史趋势 el-alert（:73-75）。
test.describe('传感器降级旅程', () => {
  test('传感器页在 InfluxDB 不可达时呈现错误警示且控件可用', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)
    await page.goto('/sensors')
    await expect(page.getByRole('heading', { name: '传感器数据' })).toBeVisible()

    // 最新读数面板出现错误警示（StateBlock error + 重试按钮）
    const latestError = page.locator('.state-block-error').first()
    await expect(latestError).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('.state-block-retry')).toBeVisible()

    // 测量项选择器正常渲染（默认选中 5 个测量项）
    await expect(page.locator('.measure-select')).toBeVisible()

    // 历史趋势区错误警示（el-alert + 重试按钮）
    const historyAlert = page.locator('.chart-panel .el-alert')
    await expect(historyAlert).toBeVisible({ timeout: 15_000 })
    await expect(historyAlert.getByRole('button', { name: '重试' })).toBeVisible()

    // 自动刷新开关可切换不崩（默认开 → 关 → 开）
    const autoRefresh = page.locator('.toolbar-right .el-switch')
    await autoRefresh.click()
    await expect(autoRefresh).not.toHaveClass(/is-checked/)
    await autoRefresh.click()
    await expect(autoRefresh).toHaveClass(/is-checked/)

    // 手动刷新按钮可用（点击不崩；错误态保持确定性）
    await page.locator('.toolbar-right button[title="刷新"]').click()
    await expect(page.locator('.state-block-error')).toBeVisible({ timeout: 15_000 })
  })
})
