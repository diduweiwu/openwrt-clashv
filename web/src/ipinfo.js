// 出口 IP 查询的共享状态：首页瓦片/hero 徽标与详情弹窗共用同一份数据。
//
// 两种检测模式（localStorage `clashv_ip_probe` 持久化，默认浏览器端）：
// - browser：在当前浏览器页面直接请求查询源。浏览器走自己的网络与代理环境，
//   检测到的就是浏览器实际出口——OpenWrt 后端发起的查询在部分网络下会失败
//   （如代理侧 502），而浏览器端检测天然走用户实际可用的链路；
// - openwrt：经插件后端 /api/ipinfo 查询（后端 60 秒缓存），直连侧走路由器
//   本机网络、代理侧经本机混合端口。
//
// 语义说明：浏览器端模式下「直连」= 国内直连可达的查询源结果，「代理」= 海外
// 查询源结果（按分流规则经代理出口，浏览器环境本身有代理时即代理出口）。
import { ref } from 'vue'
import { api } from './api.js'
import { toast } from './store.js'

export const ipData = ref(null) // {direct:{ok,ip,country,city,isp,source,error}, proxy:{...}, ts}
export const ipLoading = ref(false)

const PROBE_KEY = 'clashv_ip_probe'

// 当前检测模式：'browser' 浏览器端直查（默认）/ 'openwrt' OpenWrt 后端查
export const probeMode = ref(localStorage.getItem(PROBE_KEY) === 'openwrt' ? 'openwrt' : 'browser')

/**
 * 切换检测模式（browser ↔ openwrt）并持久化，同时清空旧数据——
 * 两种模式的直连/代理语义不同，不混用缓存。
 * @returns {string} 切换后的模式
 * @example onToggleProbe = () => { toggleProbeMode(); loadIpInfo(status?.running, true) }
 */
export function toggleProbeMode() {
  probeMode.value = probeMode.value === 'browser' ? 'openwrt' : 'browser'
  localStorage.setItem(PROBE_KEY, probeMode.value)
  ipData.value = null
  return probeMode.value
}

// ---- 浏览器端查询源 ----

// 直连源：国内直连可达、无需令牌；本插件页面为 http 内嵌，fetch http 接口无混合内容限制
const BROWSER_DIRECT = [
  {
    source: 'ip-api.com',
    url: 'http://ip-api.com/json/?lang=zh-CN&fields=status,query,country,city,isp',
    parse: async (res) => {
      const v = await res.json()
      if (v.status !== 'success' || !v.query) throw new Error('查询失败')
      return { ip: v.query, country: v.country || '', city: v.city || '', isp: v.isp || '' }
    },
  },
]

// 代理源：海外接口，按分流规则经代理出口，逐源兜底（与后端 netcheck.go 同一套源）
const BROWSER_PROXY = [
  {
    source: 'ip.sb',
    url: 'https://api.ip.sb/geoip',
    parse: async (res) => {
      const v = await res.json()
      if (!v.ip) throw new Error('响应缺少 ip 字段')
      return { ip: v.ip, country: v.country_code || v.country || '', city: v.city || '', isp: v.isp || '' }
    },
  },
  { source: 'ip.sb/ip', url: 'https://api.ip.sb/ip', parse: parseTextIp },
  { source: 'ipify', url: 'https://api.ipify.org', parse: parseTextIp },
]

/**
 * 解析纯文本 IP 端点：响应体就是 IP 本身。
 * @param {Response} res fetch 响应
 * @returns {Promise<{ip: string, country: string, city: string, isp: string}>}
 * @example parseTextIp(await fetch('https://api.ipify.org')) // → { ip: '1.2.3.4', ... }
 */
async function parseTextIp(res) {
  const ip = (await res.text()).trim()
  if (!/^[0-9a-fA-F.:]+$/.test(ip) || ip.length > 45) throw new Error('响应不是合法 IP')
  return { ip, country: '', city: '', isp: '' }
}

/**
 * 浏览器端依次请求查询源，返回第一个成功的结果；全部失败时逐源汇总原因。
 * @param {'direct'|'proxy'} kind 直连（国内源）/ 代理（海外源）侧
 * @returns {Promise<{ok: boolean, ip: string, country: string, city: string, isp: string, source: string, error: string}>}
 */
async function fetchBrowserEntry(kind) {
  const sources = kind === 'direct' ? BROWSER_DIRECT : BROWSER_PROXY
  const errs = []
  for (const src of sources) {
    try {
      // 超时 8 秒：AbortSignal.timeout 老浏览器不存在时退化为不限时（页面自身有加载超时兜底）
      const res = await fetch(src.url, { signal: AbortSignal.timeout ? AbortSignal.timeout(8000) : undefined })
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const fields = await src.parse(res)
      return { ok: true, ...fields, source: src.source, error: '' }
    } catch (e) {
      const why = e?.name === 'TimeoutError' ? '超时' : (e?.message || '网络错误')
      errs.push(src.source + ': ' + why)
    }
  }
  return { ok: false, ip: '', country: '', city: '', isp: '', source: '', error: errs.join('；') }
}

/**
 * 拉取出口 IP：按当前检测模式发起。
 * - openwrt 模式依赖后端与内核（代理侧经混合端口），内核未运行时不请求；
 * - browser 模式不经过内核，随时可查。
 * refresh=true 绕过 60 秒前端轻缓存强制重查。
 * @param {boolean} running 内核是否运行中
 * @param {boolean} [refresh=false] 是否强制重查
 * @example loadIpInfo(status?.running, true)
 */
export async function loadIpInfo(running, refresh = false) {
  if (probeMode.value === 'openwrt' && !running) return
  // 前端 60 秒轻缓存：与后端缓存节奏一致，避免反复打查询源
  if (!refresh && ipData.value?.ts && Date.now() / 1000 - ipData.value.ts < 60) return
  ipLoading.value = true
  try {
    if (probeMode.value === 'openwrt') {
      ipData.value = await api.get('/api/ipinfo' + (refresh ? '?refresh=1' : ''))
    } else {
      const [direct, proxy] = await Promise.all([fetchBrowserEntry('direct'), fetchBrowserEntry('proxy')])
      ipData.value = { direct, proxy, ts: Math.floor(Date.now() / 1000) }
    }
  } catch { /* 查询失败静默，展示占位 */ }
  finally { ipLoading.value = false }
}

// 瓦片文本：查询中/失败显示占位符
export function ipText(e) {
  return e?.ok ? e.ip : (e ? '查询失败' : '…')
}

// 位置描述：国家 · 城市 · 运营商
export function ipPlace(e) {
  if (!e?.ok) return '—'
  return [e.country, e.city, e.isp].filter(Boolean).join(' · ') || '—'
}

// 复制 IP（LuCI http 内嵌非 secure 上下文 clipboard 不可用，退 execCommand）
export function copyIp(e) {
  if (!e?.ok) return
  const done = () => toast('已复制 ' + e.ip, 'success')
  navigator.clipboard?.writeText(e.ip).then(done).catch(() => {
    const ta = document.createElement('textarea')
    ta.value = e.ip
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
    done()
  })
}
