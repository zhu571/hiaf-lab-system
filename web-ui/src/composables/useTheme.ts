// 主题单例（美术方案 §3.6 定稿）：模块级唯一一次 useColorMode 调用，
// Settings 下拉、图表组件 watch、theme-color 同步全部经本单例取值——
// 组件禁止直接调 useColorMode（vueuse 每次调用各建 useStorage 实例并独立同步 html class，
// 多实例冗余且配置易漂移）。
//
// 契约要点（§3.6）：
// - storageKey 'theme'（对齐 i18n 'language' 短键先例），localStorage 存裸三态 'light'|'dark'|'auto'；
//   localStorage 白名单仅此一键，storage 事件多标签同步为 vueuse 原生支持。
// - html.dark class 由 useColorMode 驱动，与 index.html 防闪烁脚本（只读、首帧前加 dark）收敛一致。
// - disableTransition: true（默认值，显式写出）：切换瞬间全局禁过渡，避免全站闪烁。
// - 非法存储值无行为分支（§6.1）：html 上 light/dark class 均被移除，视觉回落亮色，无注入路径。
import { watch } from 'vue'
import { useColorMode, type BasicColorSchema } from '@vueuse/core'

export type ThemeMode = BasicColorSchema // 'light' | 'dark' | 'auto'

/** theme-color 双 meta 的 canonical 值（= --bg light/dark，tokens.css / themes/dark.css 逐字一致） */
const THEME_COLOR_LIGHT = '#edf1f6'
const THEME_COLOR_DARK = '#0e1822'

const colorMode = useColorMode({
  selector: 'html',
  attribute: 'class',
  storageKey: 'theme',
  initialValue: 'auto',
  disableTransition: true
})

// theme-color 全生命周期三处职责之一「watch 同步」（另两处：index.html 双 meta media + 防闪烁脚本初始化）：
// 手动指定 light/dark 时 media 不生效，两个 meta 统一写解析色（JS 兜底）；
// auto 时恢复 canonical 双值，交还 media 承担系统级跟随。
watch(
  colorMode.state,
  (resolved) => {
    const metas = document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')
    metas.forEach((m) => {
      m.content =
        colorMode.store.value === 'auto'
          ? (m.getAttribute('media') || '').includes('dark')
            ? THEME_COLOR_DARK
            : THEME_COLOR_LIGHT
          : resolved === 'dark'
            ? THEME_COLOR_DARK
            : THEME_COLOR_LIGHT
    })
  },
  { immediate: true }
)

export function useTheme() {
  return {
    /** 三态原始值 'light'|'dark'|'auto'（Settings 下拉绑定它；写它即切换并持久化） */
    store: colorMode.store,
    /** 解析后 'light'|'dark'（图表 destroy+重建、SVG 分组色等联动 watch 它） */
    state: colorMode.state,
    /** 系统偏好 'light'|'dark'（auto 态下 state 的来源，展示用） */
    system: colorMode.system,
    /** 切换入口：写 store 即持久化 localStorage 并同步 html class / theme-color */
    setTheme(value: ThemeMode) {
      colorMode.store.value = value
    }
  }
}
