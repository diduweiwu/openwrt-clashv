// 自定义规则共享常量与目标候选拉取。
// 规则页（RulesView）与连接页「右键快速添加规则」（RuleQuickModal）共用同一份
// 类型定义与代理组拉取逻辑，避免两处枚举漂移。
import { api } from './api.js'
import { toast } from './store.js'

// 自定义规则类型全集：弹窗「类型」下拉的候选项
export const RULE_TYPES = [
  { label: 'DOMAIN（精确域名）', value: 'DOMAIN' },
  { label: 'DOMAIN-SUFFIX（域名后缀）', value: 'DOMAIN-SUFFIX' },
  { label: 'DOMAIN-KEYWORD（域名关键字）', value: 'DOMAIN-KEYWORD' },
  { label: 'DOMAIN-REGEX（域名正则）', value: 'DOMAIN-REGEX' },
  { label: 'GEOSITE（GeoSite 数据库）', value: 'GEOSITE' },
  { label: 'GEOIP（GeoIP 国家/地区码）', value: 'GEOIP' },
  { label: 'IP-CIDR（IPv4 网段）', value: 'IP-CIDR' },
  { label: 'IP-CIDR6（IPv6 网段）', value: 'IP-CIDR6' },
  { label: 'IP-ASN（ASN 编号）', value: 'IP-ASN' },
  { label: 'SRC-IP-CIDR（来源网段）', value: 'SRC-IP-CIDR' },
  { label: 'SRC-PORT（来源端口）', value: 'SRC-PORT' },
  { label: 'DST-PORT（目标端口）', value: 'DST-PORT' },
  { label: 'PROCESS-NAME（进程名）', value: 'PROCESS-NAME' },
  { label: 'MATCH（兜底，放行其余全部）', value: 'MATCH' },
]

// IP 类规则可加 no-resolve：命中时不再做 DNS 解析（路由器上推荐勾选）
export const NORESOLVE_TYPES = ['GEOIP', 'IP-CIDR', 'IP-CIDR6', 'IP-ASN']

// 值输入框按类型给示例，降低格式门槛
export const VALUE_HINT = {
  DOMAIN: 'www.example.com',
  'DOMAIN-SUFFIX': 'example.com',
  'DOMAIN-KEYWORD': 'google',
  'DOMAIN-REGEX': '^.*\\.example\\.com$',
  GEOSITE: 'youtube',
  GEOIP: 'CN',
  'IP-CIDR': '192.168.1.0/24',
  'IP-CIDR6': '2620:0:2d0::/48',
  'IP-ASN': '13335',
  'SRC-IP-CIDR': '192.168.1.0/24',
  'SRC-PORT': '7777',
  'DST-PORT': '443',
  'PROCESS-NAME': 'curl',
}

/**
 * 拉取规则「目标」候选：从内核运行时代理里筛出可导向的分组名。
 * 只返回代理组（不含 DIRECT/REJECT 兜底项，调用方按需自行拼接）。
 *
 * @returns {Promise<string[]>} 代理组名数组；内核未运行或请求失败时由调用方 catch 兜底
 * @example
 * const groups = await fetchRuleTargets().catch(() => [])
 * // groups: ['🚀 节点选择', '♻️ 自动选择', …]
 */
export async function fetchRuleTargets() {
  const data = await api.get('/api/proxies')
  return Object.values(data.proxies || {})
    .filter(p => ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay'].includes(p.type) && p.name !== 'GLOBAL')
    .map(p => p.name)
}

/**
 * 判断文本是否为 IP 地址形态（IPv4 点分十进制或含冒号的 IPv6）。
 * 连接记录的 host 字段在未嗅探出域名时可能是 IP，此时不适用域名类规则。
 *
 * @param {string} s 待判断文本
 * @returns {boolean} 是否为 IP 地址
 * @example
 * isIpText('1.2.3.4')    // true
 * isIpText('example.com') // false
 */
export function isIpText(s) {
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(s) || s.includes(':')
}

/**
 * 追加一条自定义规则：拉取现有列表 → 查重 → 追加 → 全量覆写
 * （PUT /api/rules/custom，内核运行中由后端自动重启加载）。
 * 保存结果经 toast 提示（查重命中 / 保存失败 / 内核重载状态）。
 *
 * @param {string} rule 合成好的 clash 规则串，如 'DOMAIN-SUFFIX,example.com,DIRECT'
 * @returns {Promise<boolean>} 是否保存成功；查重命中或请求失败返回 false
 * @example
 * if (await appendCustomRule('DOMAIN-SUFFIX,example.com,DIRECT')) {
 *   // 保存成功，可关闭弹窗
 * }
 */
export async function appendCustomRule(rule) {
  try {
    const cur = (await api.get('/api/rules/custom')).rules || []
    if (cur.includes(rule)) {
      toast('该规则已存在', 'warning')
      return false
    }
    const r = await api.put('/api/rules/custom', { rules: [...cur, rule] })
    if (r.error) toast('规则已添加，但内核重载失败：' + r.error, 'error', 5000)
    else toast(r.restarted ? '规则已添加，内核已重载生效' : '规则已添加，内核启动后生效', 'success')
    return true
  } catch (e) {
    toast(e.message, 'error')
    return false
  }
}
