// 测试全局基础设施（方案 §5.1）：在 vitest.config.ts 的 setupFiles 注册，
// 对每个测试文件生效一次。职责：
// 1. Element Plus 全量注册（vitest 不走 vite.config.ts 的 unplugin 按需注册，全量注册启动增量约 1s/文件）；
// 2. ElMessage / ElMessageBox 打桩为可断言 mock（真实渲染会挂 document.body 且无法断言；
//    ElMessageBox.confirm 默认 resolve 视同确认，invalidate/删除类确认流测试前提，用例内改 reject 覆盖取消分支）；
// 3. jsdom 缺失环境 stub：matchMedia / ResizeObserver / scrollIntoView；
// 4. i18n 工厂：组件测试挂真实中文消息（禁 mock t() 返回 key，与 keys.test.ts 形成双防线）。

import { config } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../i18n/zh'

// hoisted mock：测试文件经 import { ElMessage } from 'element-plus' 拿到同一份 mock 对象，
// 用例内 (ElMessage as unknown as { error: Mock }).error 断言即可。
const epMocks = vi.hoisted(() => ({
  ElMessage: {
    error: vi.fn(),
    warning: vi.fn(),
    success: vi.fn(),
    info: vi.fn()
  },
  ElMessageBox: {
    // 默认 resolve（视同用户确认）；取消分支由用例内 mockRejectedValue 覆盖
    confirm: vi.fn().mockResolvedValue('confirm'),
    alert: vi.fn().mockResolvedValue(undefined),
    prompt: vi.fn().mockResolvedValue({ value: '' })
  }
}))

// element-plus 部分 mock：组件保持真实（挂载渲染依赖），仅消息两件套打桩
vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>()
  return {
    ...actual,
    ElMessage: epMocks.ElMessage,
    ElMessageBox: epMocks.ElMessageBox
  }
})

// Element Plus 全量注册：组件测试无需逐个声明（vitest 环境没有 unplugin-vue-components）
config.global.plugins = [ElementPlus]

// i18n 工厂：组件测试用真实 zh 消息渲染；如测试需断言英文文案可在挂载时覆盖 locale/messages
export function createTestI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh',
    fallbackLocale: 'zh',
    messages: { zh }
  })
}

// ---- jsdom 缺失环境 stub（§5.1）----

// matchMedia：useMobile 的 useMediaQuery('(max-width: 768px)') 依赖；默认不匹配，测试可覆盖
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false
  })) as unknown as typeof window.matchMedia
}

// ResizeObserver：el-table / el-scrollbar 挂载依赖，jsdom 未实现
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (!('ResizeObserver' in window)) {
  ;(window as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub
}

// scrollIntoView：AskDialog 等滚动定位依赖，jsdom 无实现
Element.prototype.scrollIntoView = () => {}
