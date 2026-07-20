// Kill-switch：清理历史版本（vite-plugin-pwa 时期）注册的 Service Worker。
// 旧 SW 若不被清除会一直用缓存劫持页面，且 /sw.js 返回 HTML 时永远无法完成更新。
// 本文件被浏览器当作新 SW 安装后立即自我注销，并刷新所有受控页面。
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => {
  event.waitUntil(
    self.registration
      .unregister()
      .then(() => self.clients.matchAll({ type: 'window' }))
      .then((clients) => clients.forEach((client) => client.navigate(client.url)))
  )
})
