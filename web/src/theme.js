// 主题管理：深色 / 浅色 / 跟随系统（默认），全局单例
import { ref, watch } from 'vue'

const KEY = 'clashv_theme'

// themeMode: 用户选择的模式；resolvedTheme: 实际生效的配色
export const themeMode = ref(localStorage.getItem(KEY) || 'system')
export const resolvedTheme = ref('dark')

const mq = window.matchMedia('(prefers-color-scheme: dark)')

// ---- LuCI 内嵌主题适配 ----
// LuCI 入口页（openwrt/root/www/.../clashv/app.js）把主题背景色经 URL 参数
// luci_bg=r,g,b 传入：插件用同色铺页面底，视觉融入当前 OpenWrt 主题。
// 深浅判断：底色亮度 < 0.6 视为深色主题；「跟随系统」在内嵌时改为跟随这个
// 判断（OS 偏好让位 LuCI 主题），用户显式选的深/浅仍优先。
// 磨砂/极光不受影响：卡片半透明底与光斑伪元素都画在这层底色之上。
const luciBg = (() => {
  const m = (new URLSearchParams(window.location.search).get('luci_bg') || '').match(/^(\d{1,3}),(\d{1,3}),(\d{1,3})$/)
  if (!m) return null
  const [r, g, b] = [Number(m[1]), Number(m[2]), Number(m[3])]
  return r > 255 || g > 255 || b > 255 ? null : { r, g, b }
})()
export const luciDark = luciBg
  ? (0.299 * luciBg.r + 0.587 * luciBg.g + 0.114 * luciBg.b) / 255 < 0.6
  : null

// BG_HEX 与 index.html 防白屏内联脚本保持同一组色值：那边在页面加载时给
// <html> 写了内联 style.background（优先级高于一切 CSS），这里切换主题必须
// 同步更新它，否则 SPA 内切主题后页面背景仍停留在旧主题色——磨砂开启时
// 半透明卡片叠在旧背景上会出现「灰卡 + 文字看不见」（内联样式永远赢过类规则）
const BG_HEX = { dark: '#14161c', light: '#f3f4f8' }

export function applyTheme() {
  resolvedTheme.value = themeMode.value === 'system'
    ? (luciDark != null ? (luciDark ? 'dark' : 'light') : (mq.matches ? 'dark' : 'light'))
    : (themeMode.value === 'dark' ? 'dark' : 'light')
  document.documentElement.dataset.theme = resolvedTheme.value
  // 页面底色优先级：LuCI 传入的主题色（内嵌融合）> 内嵌透明让位（旧入口页 /
  // 主题底色取不到时的兜底，配合 style.css 的 html.embedded）> 本应用自己的
  // 主题底色（独立访问 :9097 防白屏闪色）
  if (luciBg) {
    document.documentElement.style.background = `rgb(${luciBg.r}, ${luciBg.g}, ${luciBg.b})`
  } else if (document.documentElement.classList.contains('embedded')) {
    document.documentElement.style.background = 'transparent'
  } else {
    document.documentElement.style.background = BG_HEX[resolvedTheme.value]
  }
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
