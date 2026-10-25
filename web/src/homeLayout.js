// 首页卡片布局偏好：区块顺序 + 卡片显隐（localStorage 持久化，与菜单模式/磨砂同类的
// 纯浏览器本地偏好，不走后端设置）。排序用 CSS order 实现——.page 是 flex column，
// 每个区块按 cardOrder 里的下标设 order 即可，无需改动模板结构。
//
// 区块（可排序单位）与卡片（可隐藏单位）是两层概念：
//   区块：status / profile / traffic / nodes / unlock / info（信息卡组，含三张卡）
//   卡片：上面全部 + ip / clash / sys（三张信息卡可单独隐藏，组内其余卡自动补位）
import { ref, watch } from 'vue'

const KEY = 'clashv_home_cards'

// 可排序区块定义（弹窗展示顺序即默认顺序）
export const HOME_SECTIONS = [
  { key: 'status', label: '运行状态' },
  { key: 'profile', label: '当前订阅' },
  { key: 'traffic', label: '实时流量' },
  { key: 'nodes', label: '切换节点' },
  { key: 'unlock', label: '解锁测试' },
  { key: 'info', label: '信息卡（IP / Clash / 系统）' },
]

const DEFAULT_ORDER = HOME_SECTIONS.map(s => s.key)

function load() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '{}')
    const order = Array.isArray(raw.order) ? raw.order.filter(k => DEFAULT_ORDER.includes(k)) : []
    // 版本升级新增的区块补到末尾，避免存档里没有而整块消失
    for (const k of DEFAULT_ORDER) if (!order.includes(k)) order.push(k)
    const hidden = Array.isArray(raw.hidden) ? raw.hidden.filter(k => DEFAULT_ORDER.includes(k) || ['ip', 'clash', 'sys'].includes(k)) : []
    return { order, hidden }
  } catch {
    return { order: [...DEFAULT_ORDER], hidden: [] }
  }
}

const saved = load()

// 区块顺序（key 数组，含全部区块）与隐藏的卡片 key 列表
export const cardOrder = ref(saved.order)
export const hiddenCards = ref(saved.hidden)

watch([cardOrder, hiddenCards], () => {
  try {
    localStorage.setItem(KEY, JSON.stringify({ order: cardOrder.value, hidden: hiddenCards.value }))
  } catch { /* 隐私模式等 */ }
}, { deep: true })

// 区块的 CSS order 值（直接绑到 :style）
export function orderOf(key) {
  return cardOrder.value.indexOf(key)
}

// 区块是否显示（区块本身没存 hidden，显隐都记在卡片粒度）
export function sectionVisible(key) {
  if (key === 'info') {
    return ['ip', 'clash', 'sys'].some(k => !hiddenCards.value.includes(k))
  }
  return !hiddenCards.value.includes(key)
}

// 卡片是否显示（ip/clash/sys 三卡与各自区块同 key 判定）
export function cardVisible(key) {
  return !hiddenCards.value.includes(key)
}

// 上移/下移区块：与相邻区块交换位置
export function moveSection(key, dir) {
  const arr = [...cardOrder.value]
  const i = arr.indexOf(key)
  const j = i + dir
  if (i < 0 || j < 0 || j >= arr.length) return
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  cardOrder.value = arr
}

// 切换单个卡片显隐
export function toggleCard(key, visible) {
  const set = new Set(hiddenCards.value)
  if (visible) set.delete(key)
  else set.add(key)
  hiddenCards.value = [...set]
}

// 恢复默认：默认顺序 + 全部显示
export function resetLayout() {
  cardOrder.value = [...DEFAULT_ORDER]
  hiddenCards.value = []
}
