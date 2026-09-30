<script setup>
// 连接页：内核当前活动连接的完整明细（字段对齐 Clash Verge 连接页）。
// 活跃列表来自 /api/connections（后端每秒轮询内核）；「已关闭」在本页前端追踪——
// 上一秒还在、这一秒消失的连接带最后流量快照移入已关闭列表。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api.js'
import { store, toast, fmtBytes, fmtRate } from '../store.js'

const SUBS = [
  { key: 'active', label: '活跃' },
  { key: 'closed', label: '已关闭' },
]

const sub = ref('active')
const items = ref([])
const closed = ref([])
const keyword = ref('')
const auto = ref(true)
const pollError = ref('')
const totals = ref({ up: 0, down: 0 })
const loading = ref(false)

// 上一轮采样：id → { upload, download, t }，用于逐连接速率
let prev = new Map()
// 上一轮完整记录：id → item，连接消失时带着最后流量进已关闭列表
let lastItems = new Map()
let timer = null

const shown = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  const src = sub.value === 'active' ? items.value : closed.value
  if (!k) return src
  return src.filter(c => {
    const m = c.metadata || {}
    const hay = [
      m.host, m.process, m.processPath,
      m.sourceIP, m.destinationIP,
      c.rule, c.rulePayload,
      Array.isArray(c.chains) ? c.chains.join(' ') : '',
    ].join(' ').toLowerCase()
    return hay.includes(k)
  })
})

const activeCount = computed(() => items.value.length)
const closedCount = computed(() => closed.value.length)

async function poll(silent = true) {
  if (!store.status?.running) {
    // 内核停了：清空活跃与采样，不把停机当成「全部关闭」
    prev = new Map()
    lastItems = new Map()
    items.value = []
    pollError.value = ''
    return
  }
  loading.value = !silent
  try {
    const r = await api.get('/api/connections')
    pollError.value = r.poll_error || ''
    totals.value = { up: r.up_total || 0, down: r.down_total || 0 }
    const list = r.items || []
    const now = Date.now()
    const seen = new Set()
    for (const c of list) {
      const p = prev.get(c.id)
      if (p) {
        const dt = Math.max(0.3, (now - p.t) / 1000)
        c.upSpeed = Math.max(0, (c.upload - p.upload) / dt)
        c.downSpeed = Math.max(0, (c.download - p.download) / dt)
      } else {
        c.upSpeed = 0
        c.downSpeed = 0
      }
      prev.set(c.id, { upload: c.upload, download: c.download, t: now })
      lastItems.set(c.id, c)
      seen.add(c.id)
    }
    // 消失的连接 → 已关闭（轮询出错时快照不可信，跳过本轮判定）
    if (!pollError.value) {
      for (const id of [...prev.keys()]) {
        if (seen.has(id)) continue
        prev.delete(id)
        const last = lastItems.get(id)
        lastItems.delete(id)
        if (last && !closed.value.some(x => x.id === id)) {
          closed.value.unshift({ ...last, upSpeed: 0, downSpeed: 0, closedAt: now })
        }
      }
      if (closed.value.length > 300) closed.value.length = 300
    }
    items.value = [...list].sort((a, b) => new Date(b.start) - new Date(a.start))
  } catch (e) {
    if (!silent) toast(e.message, 'error')
  } finally {
    loading.value = false
  }
}

function setAuto(on) {
  if (timer) { clearInterval(timer); timer = null }
  if (on) timer = setInterval(poll, 1000)
}

function clearClosed() {
  closed.value = []
}

async function closeAll() {
  try {
    await api.del('/api/connections')
    toast('已断开全部连接', 'success')
    await poll()
  } catch (e) {
    toast(e.message, 'error')
  }
}

function fmtDuration(iso) {
  const t = new Date(iso).getTime()
  if (!t) return '—'
  const s = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm' + (s % 60) + 's'
  return Math.floor(s / 3600) + 'h' + Math.floor((s % 3600) / 60) + 'm'
}

// 代理链 mihomo 返回 [节点…, 组]，倒序展示为 组 → 节点
function chainText(c) {
  if (!Array.isArray(c.chains) || !c.chains.length) return '—'
  return [...c.chains].reverse().join(' → ')
}

function ruleText(c) {
  if (!c.rule) return '—'
  return c.rulePayload ? `${c.rule}(${c.rulePayload})` : c.rule
}

function hostText(c) {
  const m = c.metadata || {}
  return m.host || (m.destinationIP ? `${m.destinationIP}:${m.destinationPort}` : '—')
}

function srcText(c) {
  const m = c.metadata || {}
  return m.sourceIP ? `${m.sourceIP}:${m.sourcePort}` : '—'
}

function dstText(c) {
  const m = c.metadata || {}
  return m.destinationIP ? `${m.destinationIP}:${m.destinationPort}` : '—'
}

function processText(c) {
  const m = c.metadata || {}
  return m.process || m.processPath || '—'
}

onMounted(() => {
  poll(false)
  setAuto(true)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h2 class="page-title">连接</h2>
        <p class="page-sub">
          ↑ {{ fmtBytes(totals.up) }} · ↓ {{ fmtBytes(totals.down) }} · 活跃 {{ activeCount }} 条
          <template v-if="sub === 'closed'"> · 已关闭 {{ closedCount }} 条（仅记录本页打开期间的关闭）</template>
        </p>
      </div>
      <div class="actions">
        <label class="opt"><input v-model="auto" type="checkbox" @change="setAuto(auto)"> 自动刷新</label>
        <button class="ghost sm" :disabled="loading" @click="poll(false)">刷新</button>
        <button class="danger sm" :disabled="!store.status?.running || !activeCount" @click="closeAll">关闭全部</button>
      </div>
    </div>

    <div class="toolbar card">
      <div class="subs">
        <button
          v-for="s in SUBS" :key="s.key"
          class="sub-tab" :class="{ on: sub === s.key }"
          @click="sub = s.key"
        >{{ s.label }}<span class="cnt">{{ s.key === 'active' ? activeCount : closedCount }}</span></button>
      </div>
      <div class="search">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
        </svg>
        <input v-model="keyword" type="text" placeholder="过滤：主机 / 规则 / 进程 / 地址…" spellcheck="false">
        <button v-if="sub === 'closed' && closedCount" class="ghost sm" @click="clearClosed">清空已关闭</button>
      </div>
    </div>

    <div v-if="!store.status?.running" class="card">
      <div class="empty-hint">内核未运行，启动后这里会显示连接明细</div>
    </div>
    <div v-else class="card conn-card">
      <div v-if="shown.length" class="conn-scroll">
        <table class="conn-table">
          <thead>
            <tr>
              <th>主机</th><th>下载量</th><th>上传量</th><th>下载速度</th><th>上传速度</th>
              <th>链路</th><th>规则</th><th>进程</th><th>连接时间</th>
              <th>源地址</th><th>目标地址</th><th>类型</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in shown" :key="c.id" :class="{ gone: sub === 'closed' }">
              <td class="mono host" :title="hostText(c)">{{ hostText(c) }}</td>
              <td class="mono">{{ fmtBytes(c.download) }}</td>
              <td class="mono">{{ fmtBytes(c.upload) }}</td>
              <td class="mono dl">{{ c.downSpeed ? fmtRate(c.downSpeed) : '—' }}</td>
              <td class="mono ul">{{ c.upSpeed ? fmtRate(c.upSpeed) : '—' }}</td>
              <td class="mono chain" :title="chainText(c)">{{ chainText(c) }}</td>
              <td class="mono rule" :title="ruleText(c)">{{ ruleText(c) }}</td>
              <td class="mono proc" :title="processText(c)">{{ processText(c) }}</td>
              <td class="mono">{{ fmtDuration(c.start) }}</td>
              <td class="mono addr">{{ srcText(c) }}</td>
              <td class="mono addr">{{ dstText(c) }}</td>
              <td>
                <span class="net" :class="((c.metadata?.network) || '').toLowerCase()">
                  {{ (c.metadata?.network || '—').toUpperCase() }}
                </span>
                <span v-if="c.metadata?.type" class="inbound">{{ c.metadata.type }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="pollError" class="empty-hint poll-err">⚠ {{ pollError }}</div>
      <div v-else-if="!shown.length && sub === 'active'" class="empty-hint">
        {{ keyword ? '没有匹配的连接' : '暂无活动连接，内核运行且有设备访问后这里会出现记录' }}
      </div>
      <div v-else-if="!shown.length && sub === 'closed'" class="empty-hint">
        {{ keyword ? '没有匹配的连接' : '页面打开期间关闭的连接会出现在这里' }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.page-title { margin-bottom: 2px; }
.page-sub { margin: 0; }
.actions { display: flex; align-items: center; gap: 10px; }
.opt { display: flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--text-dim); cursor: pointer; }

.toolbar {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 10px 14px; flex-wrap: wrap;
}
.subs { display: flex; gap: 4px; }
.sub-tab {
  padding: 6px 14px; border-radius: 8px; font-size: 13px;
  background: transparent; border: 1.5px solid transparent;
  color: var(--text-dim);
  display: flex; align-items: center; gap: 6px;
}
.sub-tab.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); }
.sub-tab .cnt {
  font-size: 11px; padding: 0 6px; border-radius: 99px;
  background: var(--bg-card-2); color: var(--text-dim);
}
.sub-tab.on .cnt { background: rgba(91, 107, 240, 0.18); color: var(--accent); }
.search { display: flex; align-items: center; gap: 8px; flex: 1; max-width: 420px; color: var(--text-dim); }
.search input {
  flex: 1; padding: 6px 11px; font-size: 13px; min-width: 160px;
}
.conn-card { padding: 0; overflow: hidden; }
.conn-scroll {
  max-height: calc(100vh - 250px); min-height: 240px;
  overflow: auto;
}
.conn-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.conn-table th, .conn-table td {
  padding: 7px 10px; text-align: left; white-space: nowrap;
  border-bottom: 1px solid var(--border);
}
.conn-table th {
  position: sticky; top: 0; z-index: 1;
  background: var(--bg-card); color: var(--text-dim); font-weight: 500;
}
.conn-table tbody tr:hover { background: var(--hover); }
.conn-table tbody tr.gone { opacity: 0.55; }
.conn-table .host, .conn-table .chain, .conn-table .rule, .conn-table .proc {
  max-width: 220px; overflow: hidden; text-overflow: ellipsis;
}
.conn-table .addr { color: var(--text-dim); }
.conn-table .dl { color: var(--accent); }
.conn-table .ul { color: var(--green); }
.net {
  display: inline-block; padding: 1px 7px; border-radius: 6px; font-size: 10.5px; font-weight: 600;
  background: var(--accent-soft); color: var(--accent);
}
.net.udp { background: rgba(232, 161, 60, 0.15); color: var(--orange); }
.inbound { margin-left: 6px; font-size: 10.5px; color: var(--text-dim); }
.empty-hint { color: var(--text-dim); text-align: center; padding: 60px 0; }
.empty-hint.poll-err {
  color: var(--orange); font-size: 13px; padding: 34px 16px;
  white-space: pre-wrap; word-break: break-all;
}
</style>
