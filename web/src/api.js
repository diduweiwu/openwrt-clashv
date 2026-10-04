// 轻量 API 封装：JSON 请求、错误上抛、访问令牌自动重试。
const TOKEN_KEY = 'clashv_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(t) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

async function request(method, url, body) {
  const headers = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const token = getToken()
  if (token) headers['X-Clashv-Token'] = token

  const res = await fetch(url, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  let data = {}
  try { data = await res.json() } catch { /* 空响应 */ }

  if (res.status === 401) {
    // OpenWrt 登录校验未通过：去 LuCI 登录才能解决，不该弹令牌输入框
    if (data.auth === 'luci') throw new Error(data.error || '请先登录 OpenWrt 管理后台（LuCI）')
    const input = prompt('请输入访问令牌（设置中配置的 Token）')
    if (input) {
      setToken(input)
      return request(method, url, body)
    }
    throw new Error('需要访问令牌')
  }

  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
  return data
}

export const api = {
  get: (url) => request('GET', url),
  post: (url, body = {}) => request('POST', url, body),
  put: (url, body = {}) => request('PUT', url, body),
  del: (url) => request('DELETE', url),
}
