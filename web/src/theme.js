// 主题管理：深色 / 浅色 / 跟随系统（默认），全局单例
import { ref, watch } from 'vue'

const KEY = 'clashv_theme'

// themeMode: 用户选择的模式；resolvedTheme: 实际生效的配色
export const themeMode = ref(localStorage.getItem(KEY) || 'system')
export const resolvedTheme = ref('dark')

const mq = window.matchMedia('(prefers-color-scheme: dark)')

// ---- LuCI 内嵌主题适配 ----
// 底色经三条通道到达（按优先级）：
// 1. URL 参数 luci_bg=r,g,b —— 入口页（openwrt/root/www/.../clashv/app.js）
//    按固定候选元素预估的首 paint 色，避免加载闪色；
// 2. 入口页把 iframe 挂进 LuCI 后沿真实祖先链实测主题底色，与预估不同时
//    经 postMessage(clashv_luci_bg) 校正 —— 固定候选只对 bootstrap 类主题
//    准，第三方主题的底色可能画在候选之外、或被更外层元素抢先命中；
// 3. 本模块启动后主动向入口页查询一次（clashv_bg_query），兜住「预估为空
//    但实测有色」「校正消息早于本监听注册」的时序。
// 深浅判断：底色亮度 < 0.6 视为深色主题；「跟随系统」在内嵌时改为跟随这个
// 判断（OS 偏好让位 LuCI 主题），用户显式选的深/浅仍优先。
// 磨砂/极光不受影响：卡片半透明底与光斑伪元素都画在这层底色之上。
const parseLuciBg = () => {
  const m = (new URLSearchParams(window.location.search).get('luci_bg') || '').match(/^(\d{1,3}),(\d{1,3}),(\d{1,3})$/)
  if (!m) return null
  const [r, g, b] = [Number(m[1]), Number(m[2]), Number(m[3])]
  return r > 255 || g > 255 || b > 255 ? null : { r, g, b }
}
// 可变量：祖先链实测校正会覆盖 URL 预估（export let 是活绑定，消费方自动拿到新值）
export let luciBg = parseLuciBg()
export let luciDark = luciBg
  ? (0.299 * luciBg.r + 0.587 * luciBg.g + 0.114 * luciBg.b) / 255 < 0.6
  : null

// 更新内嵌底色并重铺：入口页实测祖先链色后通知（clashv_luci_bg 消息）。
// 参数为 0–255 的 RGB 分量；重算深浅判断并立即应用新底色。
// 示例：setLuciBg(233, 236, 239) → 页面底铺 rgb(233,236,239) 并按其亮度定深浅。
function setLuciBg(r, g, b) {
  luciBg = { r, g, b }
  luciDark = (0.299 * r + 0.587 * g + 0.114 * b) / 255 < 0.6
  applyTheme()
}

if (window.self !== window.top) {
  window.addEventListener('message', (ev) => {
    if (ev.source !== window.parent) return
    const d = ev.data
    if (!d || d.type !== 'clashv_luci_bg') return
    const m = String(d.rgb || '').match(/^(\d{1,3}),(\d{1,3}),(\d{1,3})$/)
    if (!m) return
    const [r, g, b] = [Number(m[1]), Number(m[2]), Number(m[3])]
    if (r > 255 || g > 255 || b > 255) return
    setLuciBg(r, g, b)
  })
  try { window.parent.postMessage({ type: 'clashv_bg_query' }, '*') } catch (e) { /* 非同源父页拒绝时静默 */ }
}

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
