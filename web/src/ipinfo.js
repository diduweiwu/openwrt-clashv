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

export const ipData = ref(null) // {direct, proxy, ipv6, ts}——每项为 ipEntry（ok/ip/地理字段/source/error），ipv6 为本机 IPv6 出网探测
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

// 直连源：国内直连可达的纯文本 IP 回显（nipw.cn 背后端点，腾讯 EdgeOne，CORS 全开）。
// 国外源（ip-api.com 等）在部分浏览器环境会被分流走代理、查到的反而是代理出口，
// 因此直连行必须用国内源。拿到 IP 后再经 ip.sb geoip/{ip} 反查地理信息——
// 该查询查的是参数 IP 的归属，与请求走哪条链路无关。
const BROWSER_DIRECT = [
  { source: 'nipw.cn', url: 'https://4.wsmdn.top', parse: parseTextIp },
  { source: '3322', url: 'http://ip.3322.net', parse: parseTextIp },
]

// 代理源：海外接口候选池。浏览器环境无法强制某条请求走代理（某域名可能被
// 分流直连），因此并发查询全部候选、取「与直连出口不同」的第一个成功结果——
// 代理出口的定义就是与直连不同的出口；全部相同时如实展示（当前无代理分流）。
const BROWSER_PROXY = [
  {
    source: 'ip.sb',
    url: 'https://api.ip.sb/geoip',
    parse: async (res) => {
      const v = await res.json()
      if (!v.ip) throw new Error('响应缺少 ip 字段')
      return {
        ip: v.ip, country: v.country || '', countryCode: v.country_code || '',
        city: v.city || '', isp: v.isp || '', org: v.organization || '',
        asn: v.asn ? 'AS' + v.asn : '', asnOrg: v.asn_organization || '',
        timezone: v.timezone || '', lat: v.latitude != null ? String(v.latitude) : '',
        lon: v.longitude != null ? String(v.longitude) : '',
        continent: CONTINENT_CN[v.continent_code] || '',
      }
    },
  },
  {
    source: 'ip-api.com',
    url: 'http://ip-api.com/json/?lang=zh-CN&fields=status,query,country,countryCode,city,isp,org,as,timezone,lat,lon,continent',
    parse: async (res) => {
      const v = await res.json()
      if (v.status !== 'success' || !v.query) throw new Error('查询失败')
      const g = {
        ip: v.query, country: v.country || '', countryCode: v.countryCode || '',
        city: v.city || '', isp: v.isp || '', org: v.org || '',
        asn: '', asnOrg: '', timezone: v.timezone || '', continent: v.continent || '',
        lat: v.lat != null ? String(v.lat) : '', lon: v.lon != null ? String(v.lon) : '',
      }
      // as 形如「AS9808 中国移动…」：首个空格前是自治域号，其余并入自治域组织
      const asParts = String(v.as || '').trim().split(' ')
      if (asParts[0]) {
        g.asn = asParts[0]
        g.asnOrg = asParts.slice(1).join(' ')
      }
      return g
    },
  },
  { source: 'ip.sb/ip', url: 'https://api.ip.sb/ip', parse: parseTextIp },
  { source: 'ipify', url: 'https://api.ipify.org', parse: parseTextIp },
]

// IPv6 出网探测源：域名只有 AAAA 记录，本机无 IPv6 时连接直接失败；
// nipw.cn 的 6.wsmdn.top 为国内直连探测，海外源按分流规则经代理探测
const BROWSER_IPV6 = [
  { source: 'nipw.cn', url: 'https://6.wsmdn.top', parse: parseTextIp },
  { source: 'ipv6.ip.sb', url: 'https://api-ipv6.ip.sb/ip', parse: parseTextIp },
  { source: 'ipv6.ipify', url: 'https://api6.ipify.org', parse: parseTextIp },
]

// 大洲代码 → 中文名（与后端 continentCN 同表）
const CONTINENT_CN = {
  AS: '亚洲', EU: '欧洲', NA: '北美洲', SA: '南美洲',
  AF: '非洲', OC: '大洋洲', AN: '南极洲',
}

/**
 * 解析纯文本 IP 端点：响应体就是 IP 本身。
 * @param {Response} res fetch 响应
 * @returns {Promise<object>} 仅含 ip 的地理对象（其余字段空串）
 * @example parseTextIp(await fetch('https://api.ipify.org')) // → { ip: '1.2.3.4', ... }
 */
async function parseTextIp(res) {
  const ip = (await res.text()).trim()
  if (!/^[0-9a-fA-F.:]+$/.test(ip) || ip.length > 45) throw new Error('响应不是合法 IP')
  return { ip, country: '', countryCode: '', city: '', isp: '', org: '', asn: '', asnOrg: '', timezone: '', continent: '', lat: '', lon: '' }
}

/**
 * 浏览器端按序尝试查询源，返回第一个成功的结果；全部失败时逐源汇总原因。
 * （直连行、IPv6 探测用：源列表本身区分了链路语义，串行兜底即可；
 * 代理行因无法控制链路走 fetchBrowserProxy 多源并发判定。）
 * @param {Array} sources 查询源列表
 * @returns {Promise<object>} ipEntry 结构（含 ok/ip/地理字段/source/error）
 */
async function fetchBrowserEntry(sources) {
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
  return { ok: false, ip: '', country: '', countryCode: '', city: '', isp: '', org: '', asn: '', asnOrg: '', timezone: '', continent: '', lat: '', lon: '', source: '', error: errs.join('；') }
}

/**
 * 浏览器端查代理出口：并发查询全部候选源，取「与直连出口不同」的第一个成功结果
 * （按候选列表顺序优先，全字段源排前）；全部与直连相同则取第一个成功的（如实展示）。
 * @param {string} directIP 直连出口 IP（空串表示直连查询失败，退化为取第一个成功结果）
 * @returns {Promise<object>} ipEntry 结构
 */
async function fetchBrowserProxy(directIP) {
  const settled = await Promise.allSettled(
    BROWSER_PROXY.map(async (src) => {
      // 超时 8 秒：AbortSignal.timeout 老浏览器不存在时退化为不限时
      const res = await fetch(src.url, { signal: AbortSignal.timeout ? AbortSignal.timeout(8000) : undefined })
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const fields = await src.parse(res)
      return { ok: true, ...fields, source: src.source, error: '' }
    }),
  )
  const okResults = settled
    .map((r) => (r.status === 'fulfilled' ? r.value : null))
    .filter((e) => e && e.ok)
  const empty = { ok: false, ip: '', country: '', countryCode: '', city: '', isp: '', org: '', asn: '', asnOrg: '', timezone: '', continent: '', lat: '', lon: '', source: '', error: '全部查询源失败' }
  if (!okResults.length) return empty
  // 与直连不同的第一个结果即代理出口；纯文本源（无地理字段）选中时反查补全
  let pick = okResults.find((e) => directIP && e.ip !== directIP) || okResults[0]
  if (!pick.countryCode && pick.ip !== directIP) {
    try {
      const res = await fetch('https://api.ip.sb/geoip/' + encodeURIComponent(pick.ip), {
        signal: AbortSignal.timeout ? AbortSignal.timeout(8000) : undefined,
      })
      if (res.ok) {
        const v = await res.json()
        Object.assign(pick, {
          country: v.country || '', countryCode: v.country_code || '', city: v.city || '',
          isp: v.isp || '', org: v.organization || '',
          asn: v.asn ? 'AS' + v.asn : '', asnOrg: v.asn_organization || '',
          timezone: v.timezone || '', continent: CONTINENT_CN[v.continent_code] || '',
          lat: v.latitude != null ? String(v.latitude) : '', lon: v.longitude != null ? String(v.longitude) : '',
        })
      }
    } catch { /* 仅缺地理，IP 仍有效 */ }
  }
  return pick
}

/**
 * 浏览器端查直连出口：国内纯文本源拿 IP，再反查地理信息。
 * @returns {Promise<object>} ipEntry 结构（geoip 反查失败时仅缺地理字段）
 */
async function fetchBrowserDirect() {
  const direct = await fetchBrowserEntry(BROWSER_DIRECT)
  if (!direct.ok) return direct
  try {
    // api.ip.sb/geoip/{ip} 查询任意 IP 的归属，与该请求自身走哪条链路无关；
    // 失败只影响地理字段，IP 本身仍有效
    const res = await fetch('https://api.ip.sb/geoip/' + encodeURIComponent(direct.ip), {
      signal: AbortSignal.timeout ? AbortSignal.timeout(8000) : undefined,
    })
    if (!res.ok) throw new Error('HTTP ' + res.status)
    const v = await res.json()
    Object.assign(direct, {
      country: v.country || '', countryCode: v.country_code || '', city: v.city || '',
      isp: v.isp || '', org: v.organization || '',
      asn: v.asn ? 'AS' + v.asn : '', asnOrg: v.asn_organization || '',
      timezone: v.timezone || '', continent: CONTINENT_CN[v.continent_code] || '',
      lat: v.latitude != null ? String(v.latitude) : '', lon: v.longitude != null ? String(v.longitude) : '',
    })
  } catch { /* 仅缺地理，IP 仍有效 */ }
  return direct
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
      const [direct, ipv6] = await Promise.all([
        fetchBrowserDirect(),
        fetchBrowserEntry(BROWSER_IPV6),
      ])
      // 代理行的判定依赖直连结果（取与直连不同的出口），在直连查询完成后发起
      const proxy = await fetchBrowserProxy(direct.ok ? direct.ip : '')
      ipData.value = { direct, proxy, ipv6, ts: Math.floor(Date.now() / 1000) }
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
