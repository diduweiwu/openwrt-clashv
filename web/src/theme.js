// 主题管理：深色 / 浅色 / 跟随系统（默认），全局单例
import { ref, watch } from 'vue'

const KEY = 'clashv_theme'

// themeMode: 用户选择的模式；resolvedTheme: 实际生效的配色
export const themeMode = ref(localStorage.getItem(KEY) || 'system')
export const resolvedTheme = ref('dark')

const mq = window.matchMedia('(prefers-color-scheme: dark)')

export function applyTheme() {
  resolvedTheme.value = themeMode.value === 'system' ? (mq.matches ? 'dark' : 'light') : themeMode.value
  document.documentElement.dataset.theme = resolvedTheme.value
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
