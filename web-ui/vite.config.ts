import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

// 生产环境由 Go 服务器通过 r.NotFound 提供 SPA fallback：
// 所有非 /api 路径（包括 /assets/*.js、/assets/*.css）都返回 index.html，
// 没有任何静态文件路由。浏览器拿到 text/html 的“JS”会拒绝执行，导致空白页。
// 因此构建产物必须是完全自包含的单文件 index.html：JS、CSS 全部内联。
function singleFile(): Plugin {
  return {
    name: 'single-file-build',
    apply: 'build',
    enforce: 'post',
    generateBundle(_, bundle) {
      const html = bundle['index.html']
      if (!html || html.type !== 'asset') return
      let source = String(html.source)
      const inlined: string[] = []

      // 内联入口 JS；</script> 转义为 <\/script>，避免提前闭合 script 标签
      source = source.replace(/<script\b[^>]*\bsrc="([^"]+)"[^>]*><\/script>/g, (tag, src: string) => {
        const name = src.replace(/^\//, '')
        const chunk = bundle[name]
        if (!chunk || chunk.type !== 'chunk') return tag
        inlined.push(name)
        const js = chunk.code.replace(/<\/script>/g, '<\\/script>')
        return `<script type="module">${js}</script>`
      })

      // 内联 CSS
      source = source.replace(/<link\b[^>]*\bhref="([^"]+\.css)"[^>]*>/g, (tag, href: string) => {
        const name = href.replace(/^\//, '')
        const asset = bundle[name]
        if (!asset || asset.type !== 'asset') return tag
        inlined.push(name)
        return `<style>${String(asset.source)}</style>`
      })

      for (const name of inlined) delete bundle[name]
      html.source = source
    }
  }
}

export default defineConfig({
  plugins: [vue(), singleFile()],
  build: {
    // 单个 JS chunk（动态 import 全部内联），单个 CSS 文件，静态资源全部 base64 内联
    cssCodeSplit: false,
    assetsInlineLimit: 100000000,
    rollupOptions: {
      output: {
        inlineDynamicImports: true
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
