<script setup>
// 连接页：内核当前活动连接的完整明细（字段对齐 Clash Verge 连接页）。
// 活跃列表来自 /api/connections（后端每秒轮询内核）；「已关闭」在本页前端追踪——
// 上一秒还在、这一秒消失的连接带最后流量快照移入已关闭列表。
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { NButton, NCard, NCheckbox, NDataTable, NEmpty, NInput, NTag, NTabs, NTabPane } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, fmtBytes, fmtRate } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

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

const columns = [
  { title: '主机', key: 'host', className: 'mono', minWidth: 150, ellipsis: { tooltip: true }, render: hostText },
  { title: '下载量', key: 'download', width: 90, className: 'mono', render: c => fmtBytes(c.download) },
  { title: '上传量', key: 'upload', width: 90, className: 'mono', render: c => fmtBytes(c.upload) },
  { title: '下载速度', key: 'downSpeed', width: 100, className: 'mono dl', render: c => (c.downSpeed ? fmtRate(c.downSpeed) : '—') },
  { title: '上传速度', key: 'upSpeed', width: 100, className: 'mono ul', render: c => (c.upSpeed ? fmtRate(c.upSpeed) : '—') },
  { title: '链路', key: 'chains', minWidth: 140, className: 'mono', ellipsis: { tooltip: true }, render: chainText },
  { title: '规则', key: 'rule', minWidth: 110, className: 'mono', ellipsis: { tooltip: true }, render: ruleText },
  { title: '进程', key: 'process', minWidth: 110, className: 'mono', ellipsis: { tooltip: true }, render: processText },
  { title: '连接时间', key: 'start', width: 90, className: 'mono', render: (c) => fmtDuration(c.start) },
  { title: '源地址', key: 'src', width: 150, className: 'mono addr', ellipsis: { tooltip: true }, render: srcText },
  { title: '目标地址', key: 'dst', width: 150, className: 'mono addr', ellipsis: { tooltip: true }, render: dstText },
  {
    title: '类型', key: 'network', width: 90,
    render: (c) => h('span', { class: 'net-cell' }, [
      h(NTag, { size: 'tiny', bordered: false, type: c.metadata?.network === 'udp' ? 'warning' : 'primary' },
        { default: () => (c.metadata?.network || '—').toUpperCase() }),
      c.metadata?.type ? h('span', { class: 'inbound' }, c.metadata.type) : null,
    ]),
  },
]

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
  <div class="page conn-page">
    <div class="head-row">
      <div>
        <h2 class="page-title">连接</h2>
        <p class="page-sub">
          ↑ {{ fmtBytes(totals.up) }} · ↓ {{ fmtBytes(totals.down) }} · 活跃 {{ activeCount }} 条
          <template v-if="sub === 'closed'"> · 已关闭 {{ closedCount }} 条（仅记录本页打开期间的关闭）</template>
        </p>
      </div>
      <div class="actions">
        <n-checkbox v-model:checked="auto" @update:checked="setAuto">自动刷新</n-checkbox>
        <n-button size="small" :loading="loading" @click="poll(false)"><template #icon><AppIcon name="refresh" :size="13" /></template>刷新</n-button>
        <n-button size="small" type="error" ghost :disabled="!store.status?.running || !activeCount" @click="closeAll">
          <template #icon><AppIcon name="x-square" :size="13" /></template>关闭全部
        </n-button>
      </div>
    </div>

    <n-card class="toolbar">
      <div class="toolbar-row">
        <n-tabs v-model:value="sub" type="segment" size="small" class="subs">
          <n-tab-pane name="active"><template #tab>活跃 {{ activeCount }}</template></n-tab-pane>
          <n-tab-pane name="closed"><template #tab>已关闭 {{ closedCount }}</template></n-tab-pane>
        </n-tabs>
        <n-input v-model:value="keyword" size="small" placeholder="过滤：主机 / 规则 / 进程 / 地址…" clearable class="search" />
        <n-button v-if="sub === 'closed' && closedCount" size="small" @click="clearClosed"><template #icon><AppIcon name="trash" :size="13" /></template>清空已关闭</n-button>
      </div>
    </n-card>

    <n-card v-if="!store.status?.running" class="pad">
      <n-empty description="内核未运行，启动后这里会显示连接明细" />
    </n-card>
    <n-card v-else class="conn-card">
      <n-data-table
        v-if="shown.length"
        size="small"
        :columns="columns"
        :data="shown"
        :row-key="c => c.id"
        :row-class-name="c => (c.closedAt ? 'gone' : '')"
        flex-height
        :scroll-x="1370"
        class="conn-table"
      />
      <n-empty
        v-else-if="sub === 'active'"
        :description="keyword ? '没有匹配的连接' : '暂无活动连接，内核运行且有设备访问后这里会出现记录'"
        style="padding: 60px 0"
      />
      <n-empty
        v-else
        :description="keyword ? '没有匹配的连接' : '页面打开期间关闭的连接会出现在这里'"
        style="padding: 60px 0"
      />
      <div v-if="pollError" class="poll-err">⚠ {{ pollError }}</div>
    </n-card>
  </div>
</template>

<style scoped>
/* 连接页整体精确占满内容区（与日志页同理，避免 .content 外层滚动 + 表格内层
   滚动两根竖条并存）：卡片吃掉剩余高度，滚动只发生在表格内部（flex-height） */
.conn-page { height: 100%; }
.conn-card { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.conn-card :deep(.n-card-content) { flex: 1; min-height: 0; display: flex; flex-direction: column; padding: 0 2px 2px; }
.conn-table { flex: 1; min-height: 0; }
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.page-title { margin-bottom: 2px; }
.actions { display: flex; align-items: center; gap: 14px; }
.toolbar :deep(.n-card-content) { padding: 8px 14px; }
.toolbar-row { display: flex; align-items: center; gap: 12px; }
.subs { width: 220px; flex: none; }
.search { flex: 1; max-width: 420px; min-width: 160px; }
.poll-err {
  color: var(--orange); font-size: 13px; padding: 20px 16px;
  white-space: pre-wrap; word-break: break-all;
}
:deep(.gone) { opacity: 0.55; }
:deep(.dl) { color: var(--accent); }
:deep(.ul) { color: var(--green); }
:deep(.addr) { color: var(--text-dim); }
.inbound { margin-left: 6px; font-size: 10.5px; color: var(--text-dim); }

/* ---- 手机/平板：工具栏换行，搜索框独占一行；表格靠 scroll-x 横向滚动 ---- */
@media (max-width: 760px) {
  .toolbar-row { flex-wrap: wrap; }
  .toolbar-row .search { flex: 1 1 100%; max-width: 100%; }
}
</style>
