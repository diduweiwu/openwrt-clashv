// 轻量 API 封装：JSON 请求、错误上抛、访问令牌自动重试。
const TOKEN_KEY = 'clashv_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(t) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

// opts.raw：body 已经是可直接交给 fetch 的内容（如 FormData），不再做 JSON 序列化、
// 也不设 Content-Type（multipart 边界由浏览器生成）。
async function request(method, url, body, opts = {}) {
  const headers = {}
  if (body !== undefined && !opts.raw) headers['Content-Type'] = 'application/json'
  const token = getToken()
  if (token) headers['X-Clashv-Token'] = token

  const res = await fetch(url, {
    method,
    headers,
    body: body !== undefined ? (opts.raw ? body : JSON.stringify(body)) : undefined,
  })

  let data = {}
  try { data = await res.json() } catch { /* 空响应 */ }

  if (res.status === 401) {
    // OpenWrt 登录校验未通过：整页跳去 LuCI（未登录落在登录表单，登录后自动回到界面）
    if (data.auth === 'luci') {
      const target = data.login_url ||
        `${location.protocol}//${location.hostname}/cgi-bin/luci/admin/services/clashv`
      let last = 0
      try { last = +sessionStorage.getItem('clashv_auth_redirect') || 0 } catch { /* 隐私模式等 */ }
      if (Date.now() - last > 10000) {
        try { sessionStorage.setItem('clashv_auth_redirect', String(Date.now())) } catch { /* 忽略 */ }
        window.top.location.href = target
      }
      throw new Error(data.error || '请先登录 OpenWrt 管理后台（LuCI）')
    }
    const input = prompt('请输入访问令牌（设置中配置的 Token）')
    if (input) {
      setToken(input)
      return request(method, url, body, opts)
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
  // 文件上传：body 为 FormData，鉴权与 401 处理与普通请求一致
  upload: (url, formData) => request('POST', url, formData, { raw: true }),
}
