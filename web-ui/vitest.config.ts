import { defineConfig } from 'vitest/config'

// 前端单元测试配置：jsdom 环境（测试 i18n/stores/utils 等纯逻辑，不挂真实后端）。
// 与 vite.config.ts 分开：测试不需要 element-plus 按需注册等构建插件。
// 测试文件约定：web-ui/src/**/__tests__/*.test.ts（与被测源码同目录，便于就近维护）。
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/__tests__/**/*.test.ts'],
    exclude: ['node_modules/**', 'dist/**']
  }
})
