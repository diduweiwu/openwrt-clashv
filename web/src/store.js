import { reactive } from 'vue'

// 全局共享状态（规模小，不引入 pinia）
export const store = reactive({
  status: null,        // /api/status 结果
  traffic: { up: 0, down: 0, up_total: 0, down_total: 0, connections: 0, memory_mb: 0 },
  history: [],         // 最近 120 秒 {up, down}
  toasts: [],
})

let toastSeq = 0
export function toast(message, type = 'info', ms = 3200) {
  const id = ++toastSeq
  store.toasts.push({ id, message, type })
  setTimeout(() => {
    const i = store.toasts.findIndex(t => t.id === id)
    if (i >= 0) store.toasts.splice(i, 1)
  }, ms)
}

export function pushTraffic(t) {
  store.traffic = t
  store.history.push({ up: t.up, down: t.down })
  if (store.history.length > 120) store.history.shift()
}

// ---- 展示格式化 ----

export function fmtRate(bps) {
  return fmtBytes(bps) + '/s'
}

export function fmtBytes(n) {
  if (n == null || isNaN(n)) return '0 B'
  n = Number(n)
  if (n < 1024) return n.toFixed(0) + ' B'
  const units = ['KB', 'MB', 'GB', 'TB']
  let u = -1
  do { n /= 1024; u++ } while (n >= 1024 && u < units.length - 1)
  return n.toFixed(n >= 100 ? 0 : 1) + ' ' + units[u]
}

export function fmtUptime(sec) {
  sec = Number(sec) || 0
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (d > 0) return `${d}天${h}小时`
  if (h > 0) return `${h}小时${m}分`
  if (m > 0) return `${m}分${s}秒`
  return `${s}秒`
}

export function fmtTime(unix) {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString('zh-CN', { hour12: false })
}

export function delayColor(ms) {
  if (!ms || ms <= 0) return 'var(--text-dim)'
  if (ms < 200) return 'var(--green)'
  if (ms < 500) return 'var(--orange)'
  return 'var(--red)'
}
