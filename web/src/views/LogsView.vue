<script setup>
// 日志页：内核/插件日志 + 连接日志（当前活动连接，参考 OpenClash 的连接页）
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api.js'
import { toast, fmtBytes } from '../store.js'

const TABS = [
  { key: 'core', label: '内核日志', hint: 'mihomo 运行日志（logs/core.log）' },
  { key: 'plugin', label: '插件日志', hint: 'clashv 插件自身日志（logs/clashv.log）' },
  { key: 'conns', label: '连接日志', hint: '内核当前正在处理的活动连接，每 2 秒刷新' },
]

const tab = ref('core')
const content = ref('')
const exists = ref(true)
const truncated = ref(false)
const size = ref(0)
const auto = ref(true)
const follow = ref(true)
const loading = ref(false)

const conns = ref([])
const connsTotal = ref(0)

const preRef = ref(null)
let timer = null

// 大表只渲染最近 300 条，避免弱 CPU 卡顿
const shownConns = computed(() =>
  [...conns.value]
    .sort((a, b) => new Date(b.start) - new Date(a.start))
    .slice(0, 300)
)

function fmtSize(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

async function load(silent = true) {
  loading.value = !silent
  try {
    const r = await api.get(`/api/logs?kind=${tab.value}&bytes=131072`)
    const hadEnd =
      !preRef.value || preRef.value.scrollTop + preRef.value.clientHeight >= preRef.value.scrollHeight - 30
    content.value = r.content || ''
    exists.value = r.exists
    truncated.value = r.truncated
    size.value = r.size || 0
    if (follow.value && hadEnd) {
      await nextTick()
      if (preRef.value) preRef.value.scrollTop = preRef.value.scrollHeight
    }
  } catch (e) {
    if (!silent) toast(e.message, 'error')
  } finally {
    loading.value = false
  }
}

async function loadConns(silent = true) {
  try {
    const r = await api.get('/api/connections')
    connsTotal.value = (r.items || []).length
    conns.value = r.items || []
  } catch (e) {
    if (!silent) toast(e.message, 'error')
  }
}

function setAuto(on) {
  if (timer) { clearInterval(timer); timer = null }
  if (on) timer = setInterval(() => (tab.value === 'conns' ? loadConns(true) : load(true)), 2000)
}

function pick(k) {
  tab.value = k
  if (k === 'conns') {
    loadConns(false)
  } else {
    content.value = ''
    load(false)
  }
}

function startTime(iso) {
  const d = new Date(iso)
  return isNaN(d) ? '—' : d.toLocaleTimeString('zh-CN', { hour12: false })
}

function fmtDuration(iso) {
  const t = new Date(iso).getTime()
  if (!t) return '—'
  const s = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm' + (s % 60) + 's'
  return Math.floor(s / 3600) + 'h' + Math.floor((s % 3600) / 60) + 'm'
}

// 代理链 mihomo 返回 [节点…, 组]，倒序展示为 组 → 节点 更直观
function chainText(chains) {
  if (!Array.isArray(chains) || !chains.length) return '—'
  return [...chains].reverse().join(' → ')
}

function ruleText(c) {
  if (!c.rule) return '—'
  return c.rulePayload ? `${c.rule}（${c.rulePayload}）` : c.rule
}

function hostText(c) {
  const m = c.metadata || {}
  if (m.host) return m.host
  return m.destinationIP ? `${m.destinationIP}:${m.destinationPort}` : '—'
}

function srcText(c) {
  const m = c.metadata || {}
  return m.sourceIP ? `${m.sourceIP}:${m.sourcePort}` : '—'
}

onMounted(() => { load(false); setAuto(true) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div class="tabs">
        <button
          v-for="k in TABS"
          :key="k.key"
          class="tab"
          :class="{ on: tab === k.key }"
          @click="pick(k.key)"
        >{{ k.label }}</button>
      </div>
      <div class="opts">
        <label class="opt"><input v-model="auto" type="checkbox" @change="setAuto(auto)"> 自动刷新</label>
        <label v-if="tab !== 'conns'" class="opt"><input v-model="follow" type="checkbox"> 跟随滚动</label>
        <button class="ghost sm" :disabled="loading" @click="tab === 'conns' ? loadConns(false) : load(false)">刷新</button>
      </div>
    </div>

    <p class="page-sub meta-line">
      {{ TABS.find(k => k.key === tab)?.hint }}
      <template v-if="tab === 'conns'">
        · {{ connsTotal }} 个活动连接<template v-if="connsTotal > shownConns.length">（仅显示最近 {{ shownConns.length }} 条）</template>
      </template>
      <template v-else>· {{ exists ? `共 ${fmtSize(size)}` : '暂无日志' }}<template v-if="truncated">（仅显示末尾 128 KB）</template></template>
    </p>

    <div v-if="tab === 'conns'" class="card log-card">
      <div v-if="shownConns.length" class="conn-scroll">
        <table class="conn-table">
          <thead>
            <tr>
              <th>时间</th><th>时长</th><th>局域网源</th><th>访问目标</th><th>网络</th>
              <th>规则</th><th>代理链</th><th>上传</th><th>下载</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in shownConns" :key="c.id">
              <td class="mono">{{ startTime(c.start) }}</td>
              <td class="mono">{{ fmtDuration(c.start) }}</td>
              <td class="mono">{{ srcText(c) }}</td>
              <td class="mono target" :title="hostText(c)">{{ hostText(c) }}</td>
              <td>
                <span class="net" :class="(c.metadata?.network || '').toLowerCase()">
                  {{ (c.metadata?.network || '—').toUpperCase() }}
                </span>
              </td>
              <td class="mono">{{ ruleText(c) }}</td>
              <td class="mono chain" :title="chainText(c.chains)">{{ chainText(c.chains) }}</td>
              <td class="mono">{{ fmtBytes(c.upload) }}</td>
              <td class="mono">{{ fmtBytes(c.download) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty-hint">暂无活动连接，内核运行且有设备访问后这里会出现记录</div>
    </div>

    <div v-else class="card log-card">
      <pre v-if="exists && content" ref="preRef" class="log-view">{{ content }}</pre>
      <div v-else-if="exists" class="empty-hint">日志为空，启动内核后这里会有输出</div>
      <div v-else class="empty-hint">暂无日志文件</div>
    </div>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.tabs { display: flex; gap: 6px; }
.tab {
  padding: 7px 16px; border-radius: 9px; font-size: 13.5px; font-weight: 500;
  background: var(--bg-card-2); border: 1.5px solid transparent;
}
.tab.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); }
.opts { display: flex; align-items: center; gap: 14px; }
.opt { display: flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--text-dim); cursor: pointer; }
.meta-line { margin: 8px 2px 10px; }
.log-card { padding: 0; overflow: hidden; }
.log-view {
  margin: 0; padding: 14px 16px;
  font-family: var(--mono, ui-monospace, monospace);
  font-size: 12px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-all;
  max-height: calc(100vh - 210px); min-height: 300px;
  overflow-y: auto;
}
.empty-hint { color: var(--text-dim); text-align: center; padding: 60px 0; }
.conn-scroll { max-height: calc(100vh - 210px); min-height: 300px; overflow-y: auto; }
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
.conn-table .target, .conn-table .chain { max-width: 240px; overflow: hidden; text-overflow: ellipsis; }
.net {
  display: inline-block; padding: 1px 7px; border-radius: 6px; font-size: 10.5px; font-weight: 600;
  background: var(--accent-soft); color: var(--accent);
}
.net.udp { background: rgba(230, 162, 60, 0.15); color: var(--orange); }
</style>
