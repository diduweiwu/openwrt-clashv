// 磨砂模糊效果管理：开关 + 背景透明度，全局单例（localStorage 持久化，纯 UI 偏好不进后端设置）。
// 开启后 html 根节点挂 glass-on 类（样式见 style.css 的磨砂段），
// 并把用户选的「透明度」换算成背景不透明度 --glass-alpha（0~1，越小越透）供 CSS 取用。
import { ref, watch } from 'vue'

const KEY = 'clashv_glass'
const KEY_OPACITY = 'clashv_glass_opacity'

// glassOpacity 是用户语义的「透明度」（百分比）：数值越大越透，10（近乎实心）~75（很透）。
// 默认 45：背景约 55% 不透明 + 模糊，磨砂感明显且文字清晰。
export const glassEnabled = ref(localStorage.getItem(KEY) === '1')
export const glassOpacity = ref(clampOpacity(Number(localStorage.getItem(KEY_OPACITY)) || 45))

function clampOpacity(v) {
  if (!(v >= 10 && v <= 75)) return 45
  return v
}

// applyGlass 把当前状态落到文档根节点；模块加载与状态变化时都会调用。
export function applyGlass() {
  const el = document.documentElement
  el.classList.toggle('glass-on', glassEnabled.value)
  el.style.setProperty('--glass-alpha', String((100 - glassOpacity.value) / 100))
}

watch([glassEnabled, glassOpacity], () => {
  localStorage.setItem(KEY, glassEnabled.value ? '1' : '0')
  localStorage.setItem(KEY_OPACITY, String(glassOpacity.value))
  applyGlass()
})

applyGlass()
