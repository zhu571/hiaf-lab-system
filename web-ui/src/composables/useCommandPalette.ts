import { ref } from 'vue'

// 命令面板全局开关（结构改版 R2 §3.1，对齐 useAskDialog 15 行单例先例）：
// Ctrl/⌘+K 全局快捷键与顶栏搜索触发框共用同一实例，由 AppLayout 全局挂载 CommandPalette。
const paletteOpen = ref(false)

export function useCommandPalette() {
  function openPalette() {
    paletteOpen.value = true
  }
  function closePalette() {
    paletteOpen.value = false
  }
  function togglePalette() {
    paletteOpen.value = !paletteOpen.value
  }
  return { paletteOpen, openPalette, closePalette, togglePalette }
}
