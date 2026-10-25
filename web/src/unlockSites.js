// 解锁测试站点管理：内置预设 + 用户自定义站点（localStorage 持久化）。
// 自定义站点只存 {name, url}；测试结果缓存一并提供给面板首屏展示（最近一次结果，
// 重测后覆盖）。纯浏览器本地数据，不走后端设置。
import { ref, watch } from 'vue'

const CUSTOM_KEY = 'clashv_unlock_sites'
const RESULT_KEY = 'clashv_unlock_results'

// 内置预设站点：GET 经代理返回 2xx/3xx 视为「支持」，403/404/451 视为「不支持」
// （如 Netflix 对无版权区域的影片返回 404），其余状态/超时为「测试失败」。
export const PRESET_SITES = [
  { name: 'ChatGPT', url: 'https://chat.openai.com/' },
  { name: 'Claude', url: 'https://claude.ai/' },
  { name: 'Gemini', url: 'https://gemini.google.com/' },
  { name: 'Netflix', url: 'https://www.netflix.com/title/70143836' },
  { name: 'Disney+', url: 'https://www.disneyplus.com/' },
  { name: 'YouTube', url: 'https://www.youtube.com/' },
  { name: 'TikTok', url: 'https://www.tiktok.com/' },
  { name: 'Spotify', url: 'https://open.spotify.com/' },
  { name: '巴哈姆特动画疯', url: 'https://ani.gamer.com.tw/' },
  { name: '哔哩哔哩', url: 'https://www.bilibili.com/' },
]

function loadJSON(key, fallback) {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return fallback
    const v = JSON.parse(raw)
    return Array.isArray(v) ? v : fallback
  } catch {
    return fallback
  }
}

// 自定义站点列表（ref，面板与添加弹窗共用）
export const customSites = ref(loadJSON(CUSTOM_KEY, []))

watch(customSites, (v) => {
  try { localStorage.setItem(CUSTOM_KEY, JSON.stringify(v)) } catch { /* 隐私模式等 */ }
}, { deep: true })

// 最近一次测试结果：{ "name|url": {ok, code, ms, err, at} }
export const lastResults = ref(loadJSON(RESULT_KEY, {}))

export function saveResults(results) {
  for (const r of results || []) {
    lastResults.value[r.name + '|' + r.url] = { ...r, at: Math.floor(Date.now() / 1000) }
  }
  try { localStorage.setItem(RESULT_KEY, JSON.stringify(lastResults.value)) } catch { /* 忽略 */ }
}

export function resultOf(site) {
  return lastResults.value[site.name + '|' + site.url] || null
}
