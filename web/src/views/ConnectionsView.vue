<script setup>
// 连接页：内核当前活动连接的完整明细（字段对齐 Clash Verge 连接页）。
// 活跃列表来自 /api/connections（后端每秒轮询内核）；「已关闭」在本页前端追踪——
// 上一秒还在、这一秒消失的连接带最后流量快照移入已关闭列表。
// 另有：卡片全屏查看（与日志页同法）、右键连接行基于目标域名/IP 快速添加规则。
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, NCard, NCheckbox, NDataTable, NDropdown, NEmpty, NInput, NTag, NTabs, NTabPane } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, fmtBytes, fmtRate } from '../store.js'
import { fetchRuleTargets, isIpText, appendCustomRule } from '../ruleTypes.js'
import AppIcon from '../components/AppIcon.vue'
import RuleQuickModal from '../components/RuleQuickModal.vue'

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

// ---- 全屏查看：与日志页同法——系统级 Fullscreen API 优先（可盖满整个浏览器窗口，
// LuCI 内嵌需入口页 iframe 放行 allowfullscreen），不可用时回退卡片 fixed 铺满；
// Esc 退出铺满兜底态 ----
const full = ref(false)
const cardRef = ref(null)
// n-card ref 是组件实例，取 $el；普通元素直用
const cardEl = () => cardRef.value?.$el || cardRef.value

// 全屏时锁住背景滚动（手机端触摸拖动不会带动底下的页面），退出/销毁时恢复
watch(full, v => { document.body.style.overflow = v ? 'hidden' : '' })

// 系统全屏状态同步：用户按 Esc / 浏览器退出全屏时事件会把 full 拉回 false
function onFsChange() {
  full.value = document.fullscreenElement === cardEl()
}

async function toggleFull() {
  if (document.fullscreenElement) {
    try { await document.exitFullscreen() } catch { /* 浏览器自行处理 */ }
    return
  }
  try {
    await cardEl().requestFullscreen()
  } catch {
    full.value = !full.value
  }
}

// Esc 退出全屏（仅铺满兜底态响应；系统全屏态由浏览器自行退出）
function onGlobalKey(e) {
  if (e.key === 'Escape' && full.value && !document.fullscreenElement) full.value = false
}

// ---- 右键快速添加规则：基于该连接的目标域名 / IP 合成规则串，
// 一级菜单选匹配方式、二级菜单选目标（DIRECT/REJECT/代理组），点击即入列 ----
const menuShow = ref(false)
const menuX = ref(0)
const menuY = ref(0)
const menuOpts = ref([])
// 当前右键连接的候选信息（host/ip/弹窗预填推荐）：菜单回调间传递用
let ctxBase = null
// 目标候选（代理组名）：右键菜单与快速添加弹窗共用
const targets = ref([])

async function refreshTargets() {
  targets.value = await fetchRuleTargets().catch(() => [])
}

// 三段合成 clash 规则串（IP 类固定带 no-resolve：路由器上避免为匹配规则而解析 DNS）
function composeRule(type, value, target, noResolve) {
  return `${type},${value},${target}${noResolve ? ',no-resolve' : ''}`
}

// 二级菜单的目标项：DIRECT/REJECT 常用项在前，订阅代理组在后
function targetKids(base) {
  const kids = []
  for (const t of ['DIRECT', 'REJECT']) {
    kids.push({ label: t, key: `${base.key}::${t}`, rule: composeRule(base.type, base.value, t, base.noResolve) })
  }
  if (targets.value.length) kids.push({ type: 'divider', key: `${base.key}::div` })
  for (const n of targets.value) {
    kids.push({ label: n, key: `${base.key}::${n}`, rule: composeRule(base.type, base.value, n, base.noResolve) })
  }
  return kids
}

const iconOf = (name) => () => h(AppIcon, { name, size: 13 })

// 从连接记录提取规则候选：host 是嗅探域名（可能为空或恰为 IP 形态），
// ip 为目标地址；弹窗预填推荐 = 有真域名用域名后缀，否则用 IP 网段
function connRuleBase(row) {
  const m = row.metadata || {}
  const host = (m.host || '').trim()
  const ip = (m.destinationIP || '').trim()
  const hostIsIp = !!host && isIpText(host)
  const domain = host && !hostIsIp ? host : ''
  return {
    host: domain,
    ip,
    title: domain ? (ip ? `${domain} · ${ip}` : domain) : (ip || '—'),
    recommend: domain
      ? { type: 'DOMAIN-SUFFIX', value: domain, noResolve: false }
      : ip
        ? { type: ip.includes(':') ? 'IP-CIDR6' : 'IP-CIDR', value: ip.includes(':') ? `${ip}/128` : `${ip}/32`, noResolve: true }
        : { type: 'DOMAIN-SUFFIX', value: '', noResolve: false },
  }
}

// 一级菜单：顶部只读目标行 + 域名精确/域名后缀/IP 网段三组 + 自定义入口
function buildMenuOpts(base) {
  const opts = [{
    type: 'render',
    key: 'ctx-head',
    render: () => h('div', { class: 'conn-ctx-head' }, [
      h('span', { class: 'conn-ctx-label' }, '添加规则'),
      h('span', { class: 'conn-ctx-val mono' }, base.title),
    ]),
  }]
  if (base.host) {
    opts.push({ label: '域名精确匹配', key: 'g-domain', icon: iconOf('globe'), children: targetKids({ key: 'domain', type: 'DOMAIN', value: base.host }) })
    opts.push({ label: '域名后缀（含子域）', key: 'g-suffix', icon: iconOf('globe'), children: targetKids({ key: 'suffix', type: 'DOMAIN-SUFFIX', value: base.host }) })
  }
  if (base.ip) {
    const v6 = base.ip.includes(':')
    opts.push({
      label: v6 ? 'IPv6 地址（/128）' : 'IP 地址（/32）',
      key: 'g-ip', icon: iconOf('network'),
      children: targetKids({ key: 'ip', type: v6 ? 'IP-CIDR6' : 'IP-CIDR', value: v6 ? `${base.ip}/128` : `${base.ip}/32`, noResolve: true }),
    })
  }
  opts.push({ type: 'divider', key: 'd-sep' })
  opts.push({ label: '自定义…', key: 'g-custom', icon: iconOf('edit') })
  return opts
}

// 表格行右键：定位弹出菜单（下拉浮层在全屏态挂进卡片，避免被系统全屏吞掉）
function rowProps(row) {
  return {
    onContextmenu: (e) => {
      e.preventDefault()
      ctxBase = connRuleBase(row)
      menuX.value = e.clientX
      menuY.value = e.clientY
      menuOpts.value = buildMenuOpts(ctxBase)
      menuShow.value = true
    },
  }
}

// ---- 快速添加规则弹窗（「自定义…」入口）：类型/值按连接目标预填 ----
const quickShow = ref(false)
const quickInit = ref({ type: 'DOMAIN-SUFFIX', value: '', noResolve: false })

// 菜单选择分发：带 rule 的叶子项直接入列；「自定义…」开弹窗细调
async function onMenuSelect(key, option) {
  menuShow.value = false
  if (option.rule) {
    await appendCustomRule(option.rule)
    return
  }
  if (key === 'g-custom' && ctxBase) {
    quickInit.value = { ...ctxBase.recommend }
    refreshTargets() // 弹窗每次打开都刷新代理组候选，跟随当前订阅
    quickShow.value = true
  }
}

onMounted(() => {
  poll(false)
  setAuto(true)
  refreshTargets()
  window.addEventListener('keydown', onGlobalKey)
  document.addEventListener('fullscreenchange', onFsChange)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  window.removeEventListener('keydown', onGlobalKey)
  document.removeEventListener('fullscreenchange', onFsChange)
  // 系统全屏态直接切页时退出；铺满兜底态恢复背景滚动
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
  document.body.style.overflow = ''
})
</script>

<template>
  <div class="page conn-page">
    <n-card>
      <div class="head-row">
        <div class="title-line">
          <h2 class="page-title">连接</h2>
          <!-- 统计指标：标题右侧同行小字副标题（与规则/日志页 sub-note 同款） -->
          <span class="sub-note">
            ↑ {{ fmtBytes(totals.up) }} · ↓ {{ fmtBytes(totals.down) }} · 活跃 {{ activeCount }} 条
            <template v-if="sub === 'closed'"> · 已关闭 {{ closedCount }} 条</template>
          </span>
        </div>
        <div class="actions">
          <!-- 全屏查看：优先系统级全屏（盖满整个浏览器窗口，LuCI 内嵌需入口页放行），
               不可用时回退 iframe 内 fixed 铺满 -->
          <n-button size="small" circle :disabled="!store.status?.running"
            :title="full ? '退出全屏（Esc）' : '全屏查看连接'" @click="toggleFull">
            <template #icon><AppIcon :name="full ? 'shrink' : 'expand'" :size="13" /></template>
          </n-button>
          <n-checkbox v-model:checked="auto" @update:checked="setAuto">自动刷新</n-checkbox>
          <n-button size="small" :loading="loading" @click="poll(false)"><template #icon><AppIcon name="refresh" :size="13" /></template>刷新</n-button>
          <n-button size="small" type="error" ghost :disabled="!store.status?.running || !activeCount" @click="closeAll">
            <template #icon><AppIcon name="x-square" :size="13" /></template>关闭
          </n-button>
        </div>
      </div>
    </n-card>

    <n-card class="toolbar">
      <div class="toolbar-row">
        <n-tabs v-model:value="sub" type="segment" size="small" class="subs">
          <n-tab-pane name="active"><template #tab>活跃 {{ activeCount }}</template></n-tab-pane>
          <n-tab-pane name="closed"><template #tab>已关闭 {{ closedCount }}</template></n-tab-pane>
        </n-tabs>
        <n-input v-model:value="keyword" size="small" placeholder="过滤：主机 / 规则 / 进程 / 地址…" clearable class="search" />
        <n-button v-if="sub === 'closed' && closedCount" size="small" @click="clearClosed"><template #icon><AppIcon name="trash" :size="13" /></template>清空</n-button>
      </div>
    </n-card>

    <n-card v-if="!store.status?.running" class="pad">
      <n-empty description="内核未运行，启动后这里会显示连接明细" />
    </n-card>
    <n-card v-else ref="cardRef" class="conn-card" :class="{ full }">
      <!-- 连接行支持右键：基于目标域名 / IP 快速添加规则（活跃与已关闭都可加） -->
      <n-data-table
        v-if="shown.length"
        size="small"
        :columns="columns"
        :data="shown"
        :row-key="c => c.id"
        :row-class-name="c => (c.closedAt ? 'gone' : '')"
        :row-props="rowProps"
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

    <!-- 右键快速添加规则菜单：手动定位，选择或点击外部后关闭。
         全屏态浮层挂进卡片内，避免被系统全屏盖掉 -->
    <n-dropdown
      trigger="manual" :show="menuShow" :x="menuX" :y="menuY"
      :options="menuOpts" placement="bottom-start"
      :to="full ? (cardEl() || 'body') : 'body'"
      @select="onMenuSelect" @clickoutside="menuShow = false"
    />
    <!-- 快速添加规则弹窗（「自定义…」入口）：类型/值按连接目标预填 -->
    <RuleQuickModal
      v-model:show="quickShow" :init="quickInit" :targets="targets"
      :to="full ? (cardEl() || 'body') : 'body'"
    />
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
/* 统计副标题：标题右侧同行小字灰调（与规则/日志页 sub-note 同款） */
.title-line { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.sub-note { font-size: 11.5px; color: var(--text-dim); }
.page-title { margin-bottom: 2px; }
.actions { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
/* 复选框文字禁竖排挤压：放不下时整组换行而不是把「自动刷新」压成一字一行 */
.actions :deep(.n-checkbox__label) { white-space: nowrap; }
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

/* ---- 手机/平板：工具栏换行，搜索框独占一行；表格靠 scroll-x 横向滚动 ----
   页面解锁一屏（与日志页同法）：头部/工具栏换行后会把 flex:1 的表格压缩到 0，
   改页面自然高度 + 表格定高 60vh 内部滚动（flex-height 依赖容器高度） */
@media (max-width: 760px) {
  .conn-page { height: auto; }
  .conn-card { flex: none; }
  /* flex:1 的 basis=0 在自动高度父容器里会盖掉 height，须放开 flex 用定高 */
  .conn-table { flex: none; height: 60vh; min-height: 300px; }
  .toolbar-row { flex-wrap: wrap; }
  .toolbar-row .search { flex: 1 1 100%; max-width: 100%; }
}

/* ---- 全屏查看（与日志页 .log-card.full 同法，置于媒体查询之后保证覆盖手机定高）：
   fixed 铺满视口 + 实心底盖掉磨砂半透明；表格恢复弹性占满 ---- */
.conn-card.full {
  position: fixed; top: 0; right: 0; bottom: 0; left: 0; z-index: 1000;
  border-radius: 0; border: none;
  background: var(--bg);
}
.conn-card.full .conn-table { flex: 1; height: auto; }
</style>

<style>
/* 右键菜单顶部目标行：NDropdown 浮层 teleport 到 body/卡片，scoped 属性不跟随，
   须放全局样式块（类名带 conn- 前缀防冲突） */
.conn-ctx-head {
  display: flex; align-items: baseline; gap: 10px;
  max-width: 380px; padding: 2px 0;
}
.conn-ctx-label { font-size: 11.5px; color: var(--text-dim); flex: none; }
.conn-ctx-val {
  font-size: 12px; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
</style>
