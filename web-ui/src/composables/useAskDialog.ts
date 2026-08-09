import { ref } from 'vue'

// AI 问答抽屉的全局开关：桌面侧栏项与移动端顶栏按钮共用同一实例，
// 由 AppLayout 持有并渲染 AskDialog，避免路由辅助页与重复实例。
const askOpen = ref(false)

export function useAskDialog() {
  function openAskDialog() {
    askOpen.value = true
  }
  function closeAskDialog() {
    askOpen.value = false
  }
  return { askOpen, openAskDialog, closeAskDialog }
}
