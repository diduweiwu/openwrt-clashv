<script setup>
// 日志页：内核日志 + 插件日志（连接明细已独立为「连接」页）
// 行渲染：时间/level 拆成彩色胶囊 tag，支持关键字、时间范围、级别筛选，
// 筛选纯前端做（后端只给尾部 128KB 文本，数据量小）
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, NCard, NCheckbox, NDatePicker, NEmpty, NInput, NSelect, NTabs, NTab } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

const TABS = [
  { key: 'core', label: '内核日志', hint: 'mihomo 运行日志（logs/core.log）' },
  { key: 'plugin', label: '插件日志', hint: 'clashv 插件自身日志（logs/clashv.log）' },
]

const tab = ref('core')
const raw = ref('')
const exists = ref(true)
const truncated = ref(false)
const size = ref(0)
const auto = ref(true)
const follow = ref(true)
const loading = ref(false)

// 筛选条件：关键字 / 级别 / 时间范围（[起, 止] 毫秒时间戳，null=不限）
const kw = ref('')
const levelSel = ref(null)
const range = ref(null)

const preRef = ref(null)
let timer = null

function fmtSize(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

// ---- 行解析 ----
// 行首时间戳：内核日志 time="2026-…Z"（logrus 带引号）、插件日志 time="…"（slog），
// 统一换算为本地「年-月-日 时:分:秒.毫秒」。内核若还在写 UTC（Z 结尾），浏览器
// 会换算成本地时区，时区错乱在界面上不可见
const TIME_RE = /^time=(?:"([^"]+)"|(\S+))/
const LEVEL_RE = /^level=(\w+)\s*/
const MSG_Q_RE = /^msg="((?:[^"\\]|\\.)*)"\s*/
const MSG_B_RE = /^msg=(\S+)\s*/

function unescapeMsg(s) {
  return s.replace(/\\n/g, '\n').replace(/\\t/g, '\t').replace(/\\([\\"])/g, '$1')
}

function parseLine(line) {
  const o = { raw: line, t: null, ts: '', level: '', msg: line, attrs: '' }
  let rest = line
  const m = TIME_RE.exec(line)
  if (m) {
    // RFC3339 带纳秒（9 位小数）时截到毫秒，超出 JS Date 解析精度
    const r = (m[1] || m[2] || '').replace(/\.(\d{3})\d+/, '.$1')
    const d = new Date(r)
    if (!isNaN(d)) {
      const p = (n, l = 2) => String(n).padStart(l, '0')
      o.t = d.getTime()
      o.ts = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
        `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
      rest = line.slice(m[0].length).replace(/^ /, '')
    }
  }
  const lm = LEVEL_RE.exec(rest)
  if (lm) {
    o.level = lm[1].toLowerCase()
    rest = rest.slice(lm[0].length)
    const qm = MSG_Q_RE.exec(rest)
    if (qm) {
      o.msg = unescapeMsg(qm[1])
      o.attrs = rest.slice(qm[0].length)
    } else {
      const bm = MSG_B_RE.exec(rest)
      if (bm) {
        o.msg = bm[1]
        o.attrs = rest.slice(bm[0].length)
      } else {
        o.msg = rest
      }
    }
  } else {
    o.msg = rest // 有时间无 level（或整行都不是日志格式）
  }
  return o
}

const parsed = computed(() => {
  const arr = raw.value.split('\n')
  if (arr.length && arr[arr.length - 1] === '') arr.pop()
  return arr.map(parseLine)
})

const LEVEL_OPTS = [
  { label: '全部级别', value: null },
  { label: 'DEBUG', value: 'debug' },
  { label: 'INFO', value: 'info' },
  { label: 'WARN', value: 'warning' },
  { label: 'ERROR', value: 'error' },
]
function levelLabel(l) {
  return l === 'warning' ? 'WARN' : l.toUpperCase()
}

const filtered = computed(() => {
  const k = kw.value.trim().toLowerCase()
  const lv = levelSel.value
  const rg = range.value
  return parsed.value.filter(l =>
    (!k || l.raw.toLowerCase().includes(k)) &&
    (!lv || l.level === lv) &&
    (!rg || (l.t != null && l.t >= rg[0] && l.t <= rg[1])))
})

// ---- 加载与滚动 ----
async function load(silent = true) {
  loading.value = !silent
  try {
    const r = await api.get(`/api/logs?kind=${tab.value}&bytes=131072`)
    const hadEnd =
      !preRef.value || preRef.value.scrollTop + preRef.value.clientHeight >= preRef.value.scrollHeight - 30
    raw.value = r.content || ''
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

function setAuto(on) {
  if (timer) { clearInterval(timer); timer = null }
  if (on) timer = setInterval(() => load(true), 2000)
}

function pick(k) {
  tab.value = k
  raw.value = ''
  load(false)
}

// 筛选变化后按跟随状态归位：跟随滚动则落到底部，否则保持当前位置
watch([kw, levelSel, range], async () => {
  if (!follow.value) return
  await nextTick()
  if (preRef.value) preRef.value.scrollTop = preRef.value.scrollHeight
})

function jump(top) {
  if (!preRef.value) return
  preRef.value.scrollTop = top ? 0 : preRef.value.scrollHeight
}

async function clearLog() {
  const label = TABS.find(k => k.key === tab.value)?.label || '日志'
  if (!(await ask('清空日志', `确定清空「${label}」文件的全部内容？此操作不可恢复。`))) return
  try {
    await api.post(`/api/logs/clear?kind=${tab.value}`)
    toast(`${label}已清空`, 'success')
    await load(false)
  } catch (e) {
    toast(e.message, 'error')
  }
}

onMounted(() => { load(false); setAuto(true) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="page logs-page">
    <div class="head-row">
      <div>
        <h2 class="page-title">日志</h2>
        <p class="page-sub">
          {{ TABS.find(k => k.key === tab)?.hint }} ·
          {{ exists ? `共 ${fmtSize(size)}` : '暂无日志' }}<template v-if="truncated">（仅显示末尾 128 KB）</template>
        </p>
      </div>
      <n-tabs :value="tab" type="segment" size="small" class="tabs" @update:value="pick">
        <!-- NTab 纯展示标签：NTabPane 会渲染空内容区，在标签下多出一大块高度 -->
        <n-tab v-for="k in TABS" :key="k.key" :name="k.key">{{ k.label }}</n-tab>
      </n-tabs>
      <div class="opts">
        <n-checkbox v-model:checked="auto" @update:checked="setAuto">自动刷新</n-checkbox>
        <n-checkbox v-model:checked="follow">跟随滚动</n-checkbox>
        <n-button size="small" :loading="loading" @click="load(false)"><template #icon><AppIcon name="refresh" :size="13" /></template>刷新</n-button>
      </div>
    </div>

    <n-card class="log-card">
      <!-- 筛选与快捷操作条 -->
      <div class="log-bar">
        <n-input v-model:value="kw" size="small" clearable placeholder="关键字筛选…" class="f-kw">
          <template #prefix>
            <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
            </svg>
          </template>
        </n-input>
        <n-select v-model:value="levelSel" size="small" :options="LEVEL_OPTS" placeholder="筛选级别" class="f-level" />
        <n-date-picker
          v-model:value="range" type="datetimerange" size="small" clearable
          format="MM-dd HH:mm" class="f-range"
        />
        <span class="lg-count mono">{{ filtered.length }}/{{ parsed.length }} 行</span>
        <div class="bar-btns">
          <n-button size="small" title="瞬移到顶部" @click="jump(true)"><template #icon><AppIcon name="chevron-up" :size="14" /></template></n-button>
          <n-button size="small" title="瞬移到底部" @click="jump(false)"><template #icon><AppIcon name="chevron-down" :size="14" /></template></n-button>
          <n-button size="small" type="error" ghost @click="clearLog">
            <template #icon><AppIcon name="trash" :size="13" /></template>清空
          </n-button>
        </div>
      </div>

      <div v-if="exists && raw" ref="preRef" class="log-view">
        <div v-for="(l, i) in filtered" :key="i" class="lg-line">
          <template v-if="l.ts">
            <span class="lg-t">{{ l.ts }}</span><span v-if="l.level" class="lg-l" :class="'lvl-' + l.level">{{ levelLabel(l.level) }}</span><span class="lg-m">{{ l.msg }}</span><span v-if="l.attrs" class="lg-a">{{ l.attrs }}</span>
          </template>
          <span v-else class="lg-m">{{ l.raw || ' ' }}</span>
        </div>
        <div v-if="!filtered.length" class="lg-none">没有匹配的日志行（共 {{ parsed.length }} 行被过滤）</div>
      </div>
      <div v-else-if="exists" class="log-empty">
        <n-empty description="日志为空，启动内核后这里会有输出" style="padding: 60px 0" />
      </div>
      <div v-else class="log-empty">
        <n-empty description="暂无日志文件" style="padding: 60px 0" />
      </div>
    </n-card>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.tabs { width: 260px; flex: none; margin-left: auto; }
.opts { display: flex; align-items: center; gap: 14px; }
/* 日志页整体精确占满内容区（.content 有 overflow:auto，若页面超高会同时出现
   内外两根滚动条）：page 高度锁死为可视区，卡片吃掉剩余高度，滚动只发生在
   日志组件内部；双类名覆盖全局 .page 的 16px gap——块间呼吸靠 head-row 的
   margin-bottom 提供，0 间隙贴紧 */
.page.logs-page { height: 100%; gap: 0; }
/* 手机端与设置页对齐：tabs 换行后靠左（桌面的 margin-left:auto 只是把它推到标题行右侧） */
@media (max-width: 760px) {
  .tabs { margin-left: 0; }
}
.log-card { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.log-card :deep(.n-card-content) { padding: 0; flex: 1; min-height: 0; display: flex; flex-direction: column; }

/* 筛选/操作条 */
.log-bar {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 10px 12px; border-bottom: 1px solid var(--border);
}
.f-kw { flex: 1 1 150px; max-width: 240px; }
.f-level { width: 112px; flex: none; }
.f-range { width: 310px; flex: none; }
.lg-count { flex: none; color: var(--text-dim); font-size: 11px; }
.bar-btns { display: flex; gap: 8px; margin-left: auto; }

.log-view {
  flex: 1; min-height: 0; box-sizing: border-box;
  padding: 12px 16px;
  font-family: var(--mono, ui-monospace, monospace);
  font-size: 12px; line-height: 1.75;
  overflow-y: auto;
}
.log-empty { flex: 1; min-height: 0; overflow-y: auto; }
.lg-line { white-space: pre-wrap; word-break: break-all; min-height: 1.75em; }
.lg-m { color: var(--text); }
.lg-a { color: var(--text-dim); font-size: 11px; margin-left: 8px; }
.lg-none { color: var(--text-dim); padding: 20px 0; }

/* 时间/级别胶囊 */
.lg-t, .lg-l {
  display: inline-block; vertical-align: baseline;
  font-size: 10px; line-height: 1; padding: 3px 6px;
  border-radius: 5px; margin-right: 6px;
}
.lg-t { background: var(--hover); color: var(--text-dim); }
.lg-l { font-weight: 600; letter-spacing: 0.3px; background: var(--hover); color: var(--text-dim); }
.lvl-info { background: var(--accent-soft); color: #93a0ff; }
.lvl-warning { background: rgba(232, 161, 60, 0.16); color: #eda94e; }
.lvl-error { background: rgba(224, 85, 85, 0.18); color: #f08585; }
html[data-theme='light'] .lvl-info { color: #4353d8; }
html[data-theme='light'] .lvl-warning { color: #a8721c; }
html[data-theme='light'] .lvl-error { color: #bb3a3a; }
</style>
