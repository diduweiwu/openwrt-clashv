// 出口 IP 查询的共享状态：首页 hero 徽标与「IP 信息」卡片共用同一份数据，
// 避免两处各自轮询/重复请求（后端本身也有 60s 缓存，这里省一次往返）。
import { ref } from 'vue'
import { api } from './api.js'
import { toast } from './store.js'

export const ipData = ref(null) // {direct:{ip,country,city,isp,source,ok}, proxy:{...}, ts}
export const ipLoading = ref(false)

// 拉取出口 IP：running=false 时不请求（内核未运行代理端口连不通）。
// refresh=true 绕过后端 60s 缓存强制重查。
export async function loadIpInfo(running, refresh = false) {
  if (!running) return
  ipLoading.value = true
  try {
    ipData.value = await api.get('/api/ipinfo' + (refresh ? '?refresh=1' : ''))
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
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
    done()
  })
}
