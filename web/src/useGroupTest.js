// 组节点测速（代理页与首页「切换节点」共用）：
// 路由器 CPU 弱，内核的整组测速接口会瞬间并发测所有节点导致全部超时，
// 这里改为逐节点测速、限制并发，结果实时回填到 proxies，页面延迟展示随之刷新。
import { ref } from 'vue'
import { api } from './api.js'
import { toast } from './store.js'

// 会被当作「分组」处理的节点类型（分组的 all 里可能套别的分组，测速时要跳过）
export const GROUP_TYPES = ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay']

/**
 * 创建「整组测速」控制器。
 * @param {import('vue').Ref<Object>} proxies 内核 /api/proxies 的响应式字典，
 *   测出的延迟会实时回填到各节点的 history（与 delayOf 的读取路径一致）。
 * @returns {{
 *   testing: import('vue').Ref<string>,
 *   testProg: import('vue').Ref<string>,
 *   testGroup: (g: {name: string, all: string[]}) => Promise<void>,
 * }} testing 为正在测速的组名（空串表示空闲），testProg 为「已完成/总数」进度文案。
 *   同一时刻只允许一组在测（testing 非空时其余按钮应禁用），禁用 UI 由调用方负责。
 * @example
 * const proxies = ref({})
 * const { testing, testProg, testGroup } = useGroupTest(proxies)
 * // 组头按钮：
 * // <n-button :loading="testing === g.name" :disabled="testing !== ''" @click="testGroup(g)">
 * //   {{ testing === g.name ? `测速中 ${testProg}` : '整组测速' }}
 * // </n-button>
 */
export function useGroupTest(proxies) {
  const testing = ref('') // 正在测速的组名
  const testProg = ref('') // 测速进度 "已完成/总数"

  /**
   * 测一整组：过滤掉组中组，5 并发逐个测延迟并回填 history。
   * @param {{name: string, all: string[]}} g 代理组（all 为节点名列表）
   * @returns {Promise<void>} 完成后弹结果 toast；单个节点失败按超时处理不打断整体
   */
  async function testGroup(g) {
    testing.value = g.name
    const nodes = g.all.filter(n => {
      const t = proxies.value[n]?.type
      return t && !GROUP_TYPES.includes(t)
    })
    const total = nodes.length
    let done = 0
    let ok = 0
    const CONCURRENCY = 5
    const workers = Array.from({ length: Math.min(CONCURRENCY, nodes.length) }, async () => {
      while (nodes.length) {
        const node = nodes.shift()
        try {
          // 8s：弱 CPU 上 TLS 握手 + 代理链路往返较慢，5s 会把可用节点误判为超时
          const r = await api.get(
            `/api/proxies/${encodeURIComponent(node)}/delay?timeout=8000`
          )
          const delay = r?.delay || 0
          if (delay > 0) ok++
          const p = proxies.value[node]
          if (p && delay > 0) {
            const history = Array.isArray(p.history) ? p.history : []
            history.push({ time: new Date().toISOString(), delay })
            p.history = history.slice(-10)
          }
        } catch { /* 单节点失败按超时处理，不打断整体 */ }
        done++
        testProg.value = `${done}/${total}`
      }
    })
    try {
      await Promise.all(workers)
      toast(`「${g.name}」测速完成：${ok}/${total} 个节点可用`, ok > 0 ? 'success' : 'error', 5000)
    } finally {
      testing.value = ''
      testProg.value = ''
    }
  }

  return { testing, testProg, testGroup }
}
