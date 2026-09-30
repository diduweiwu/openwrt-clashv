import { computed, reactive } from 'vue'
import { createDiscreteApi, darkTheme } from 'naive-ui'
import { resolvedTheme } from './theme.js'

// 全局共享状态（规模小，不引入 pinia）
export const store = reactive({
  status: null,        // /api/status 结果
  traffic: { up: 0, down: 0, up_total: 0, down_total: 0, connections: 0, memory_mb: 0 },
  history: [],         // 最近 120 秒 {up, down}
})

// ---- Naive UI 主题（跟随 resolvedTheme，App 与离散 API 共用同一份） ----
export const naiveTheme = computed(() => (resolvedTheme.value === 'dark' ? darkTheme : null))

export const naiveOverrides = computed(() => {
  const dark = resolvedTheme.value === 'dark'
  return {
    common: {
      fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, sans-serif",
      borderRadius: '9px',
      borderRadiusSmall: '7px',
      primaryColor: '#5b6bf0',
      primaryColorHover: '#6f7cf2',
      primaryColorPressed: '#4a59d6',
      primaryColorSuppl: '#5b6bf0',
      successColor: '#3fb96f',
      successColorHover: '#55c47f',
      successColorPressed: '#35a55f',
      successColorSuppl: '#3fb96f',
      warningColor: '#e8a13c',
      warningColorHover: '#ecb35e',
      warningColorPressed: '#d18f2f',
      warningColorSuppl: '#e8a13c',
      errorColor: '#e05555',
      errorColorHover: '#e66e6e',
      errorColorPressed: '#cd4545',
      errorColorSuppl: '#e05555',
      bodyColor: dark ? '#14161c' : '#f3f4f8',
      cardColor: dark ? '#1e222c' : '#ffffff',
      popoverColor: dark ? '#242938' : '#ffffff',
      inputColor: dark ? '#242938' : '#f5f6fa',
      actionColor: dark ? '#242938' : '#f5f6fa',
      modalColor: dark ? '#1e222c' : '#ffffff',
      borderColor: dark ? 'rgba(255, 255, 255, 0.07)' : 'rgba(20, 22, 30, 0.08)',
      dividerColor: dark ? 'rgba(255, 255, 255, 0.07)' : 'rgba(20, 22, 30, 0.08)',
    },
    Card: { borderRadius: '14px', paddingMedium: '18px 20px' },
  }
})

// 离散 API：toast/confirm 在任意模块可用，不依赖组件挂载；主题与 App 同步
const { message, dialog } = createDiscreteApi(['message', 'dialog'], {
  configProviderProps: computed(() => ({ theme: naiveTheme.value, themeOverrides: naiveOverrides.value })),
})

// 与旧版手写 toast 同签名的封装，视图层无需改动
export function toast(messageText, type = 'info', ms = 3200) {
  message[type](messageText, {
    duration: ms,
    closable: type === 'error' || type === 'warning',
  })
}

// 替代 window.confirm 的确认弹窗，返回 Promise<boolean>
export function ask(title, content) {
  return new Promise((resolve) => {
    let done = false
    const settle = (v) => { if (!done) { done = true; resolve(v) } }
    dialog.warning({
      title,
      content,
      positiveText: '确认',
      negativeText: '取消',
      onPositiveClick: () => settle(true),
      onNegativeClick: () => settle(false),
      onClose: () => settle(false),
      onMaskClick: () => settle(false),
      onEsc: () => settle(false),
    })
  })
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
