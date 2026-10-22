// 主题管理：深色 / 浅色 / 跟随系统（默认），全局单例
import { ref, watch } from 'vue'

const KEY = 'clashv_theme'

// themeMode: 用户选择的模式；resolvedTheme: 实际生效的配色
export const themeMode = ref(localStorage.getItem(KEY) || 'system')
export const resolvedTheme = ref('dark')

const mq = window.matchMedia('(prefers-color-scheme: dark)')

// BG_HEX 与 index.html 防白屏内联脚本保持同一组色值：那边在页面加载时给
// <html> 写了内联 style.background（优先级高于一切 CSS），这里切换主题必须
// 同步更新它，否则 SPA 内切主题后页面背景仍停留在旧主题色——磨砂开启时
// 半透明卡片叠在旧背景上会出现「灰卡 + 文字看不见」（内联样式永远赢过类规则）
const BG_HEX = { dark: '#14161c', light: '#f3f4f8' }

export function applyTheme() {
  resolvedTheme.value = themeMode.value === 'system' ? (mq.matches ? 'dark' : 'light') : themeMode.value
  document.documentElement.dataset.theme = resolvedTheme.value
  document.documentElement.style.background = BG_HEX[resolvedTheme.value]
}

function onSystemChange() {
  if (themeMode.value === 'system') applyTheme()
}
if (typeof mq.addEventListener === 'function') {
  mq.addEventListener('change', onSystemChange)
}

watch(themeMode, (v) => {
  localStorage.setItem(KEY, v)
  applyTheme()
})

applyTheme()
