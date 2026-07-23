import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

// 生产环境由 Go 服务器提供：存在的文件（/assets/*.js、*.css）直接返回，
// 其余非 /api 路径回退到 index.html（见 go-server/main.go 的 NotFound 处理）。
// 因此构建产物可以是标准多文件：路由懒加载 + vendor 分包 + 自动 modulepreload。
export default defineConfig({
  plugins: [
    vue(),
    // element-plus 按需注册组件并自动引入对应 CSS（含 v-loading 指令）
    Components({
      resolvers: [ElementPlusResolver({ importStyle: 'css', directives: true })]
    })
  ],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          // 图标库单独分包（element-plus 内部不依赖它）
          if (id.includes('@element-plus/icons-vue')) return 'vendor-icons'
          // element-plus 及其嵌套依赖（element-plus/node_modules/...）单独分包
          if (id.includes('element-plus')) return 'vendor-element'
          // vue / vue-router / pinia / axios / @vueuse 等其余依赖
          return 'vendor'
        }
      }
    }
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8000'
    }
  }
})
