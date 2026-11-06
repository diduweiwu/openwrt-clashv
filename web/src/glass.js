// 磨砂模糊效果管理：开关 + 背景透明度，全局单例（localStorage 持久化，纯 UI 偏好不进后端设置）。
// 开启后 html 根节点挂 glass-on 类（样式见 style.css 的磨砂段），
// 并把用户选的「透明度」换算成背景不透明度 --glass-alpha（0~1，越小越透）供 CSS 取用。
import { ref, watch } from 'vue'

const KEY = 'clashv_glass'
const KEY_OPACITY = 'clashv_glass_opacity'
const KEY_MOTION = 'clashv_glass_motion'
const KEY_LIGHTS = 'clashv_glass_lights'

// glassOpacity 是用户语义的「透明度」（百分比）：数值越大越透，10（近乎实心）~100（全透纯磨砂）。
// 默认 65：磨砂面透感明显（参考同类面板的通透观感），文字仍靠磨砂模糊与卡片内光斑衬托。
export const glassEnabled = ref(localStorage.getItem(KEY) === '1')
export const glassOpacity = ref(clampOpacity(Number(localStorage.getItem(KEY_OPACITY)) || 65))
// glassLights 控制卡片/侧栏内光斑渐变（默认开）：关=面板退回纯 tint 磨砂。
// 光斑画在面板自身 ::before 伪元素上（style.css 的 .n-card/.sidebar 规则），
// 随面板圆角裁剪、只在面板内可见，不染页面背景；背景恒为主题底色（内嵌透明
// 时透 LuCI 真实背景）。存 '0' 为关，缺省/其他值均为开。
export const glassLights = ref(localStorage.getItem(KEY_LIGHTS) !== '0')
// glassMotion 控制光斑游走动画（默认开）：光斑层以 transform 缓慢漂移+微缩放
// （合成器动画，不触发重绘；且光斑层在 backdrop 采样区之后绘制，不会连累磨砂
// 逐帧重采样——与旧版页面级光斑的卡顿机理不同）。关=光斑停在原位。
// 存 '0' 为关，缺省/其他值均为开。
export const glassMotion = ref(localStorage.getItem(KEY_MOTION) !== '0')

function clampOpacity(v) {
  if (!(v >= 10 && v <= 100)) return 65
  return v
}

// applyGlass 把当前状态落到文档根节点；模块加载与状态变化时都会调用。
export function applyGlass() {
  const el = document.documentElement
  el.classList.toggle('glass-on', glassEnabled.value)
  el.classList.toggle('glass-motion-off', !glassMotion.value)
  el.classList.toggle('glass-lights-off', !glassLights.value)
  el.style.setProperty('--glass-alpha', String((100 - glassOpacity.value) / 100))
  randomizeAurora(document)
}

// ---- 光斑初始参数真随机 ----
// CSS nth 变体只能按容器索引伪随机分布，而卡片在容器里混排着 hero/列表等非
// 卡片元素，索引不可控——一批卡片可能整批落进同一变体组（同相位同落点，首次
// 出现位置一致）。这里用 JS 给每个面板注入内联随机值：负 delay=从动画随机相位
// 起播（首次出现位置即随机）、落点/周期各面板独立随机，每次刷新都不同。
// 内联样式优先级高于样式表，注入后自然覆盖 nth 变体（nth 仅作注入前首帧兜底）。
// dataset.cauDone 打标防重复；Vue 动态渲染的新卡片由 MutationObserver 兜住。
function randomizeAurora(root) {
  root.querySelectorAll?.('.n-card, .sidebar').forEach((el) => {
    if (el.dataset.cauDone) return
    el.dataset.cauDone = '1'
    const rnd = (min, max) => (min + Math.random() * (max - min)).toFixed(1)
    el.style.setProperty('--cau-delay', `-${rnd(0, 20)}s`)
    el.style.setProperty('--cau-dur', `${rnd(14, 20)}s`)
    el.style.setProperty('--cau-pos-1', `${rnd(10, 60) | 0}% ${rnd(8, 55) | 0}%`)
    el.style.setProperty('--cau-pos-2', `${rnd(40, 90) | 0}% ${rnd(45, 92) | 0}%`)
  })
}

let cauRaf = 0
// DOM 增删后补注入（rAF 合并高频变更，连接页等每秒刷新的页面不产生额外开销）；
// 只在有新增节点时扫描，纯文本/属性更新不触发
new MutationObserver((muts) => {
  if (!muts.some((m) => m.addedNodes.length)) return
  cancelAnimationFrame(cauRaf)
  cauRaf = requestAnimationFrame(() => randomizeAurora(document))
}).observe(document.documentElement, { childList: true, subtree: true })

watch([glassEnabled, glassOpacity, glassMotion, glassLights], () => {
  localStorage.setItem(KEY, glassEnabled.value ? '1' : '0')
  localStorage.setItem(KEY_OPACITY, String(glassOpacity.value))
  localStorage.setItem(KEY_MOTION, glassMotion.value ? '1' : '0')
  localStorage.setItem(KEY_LIGHTS, glassLights.value ? '1' : '0')
  applyGlass()
})

applyGlass()
