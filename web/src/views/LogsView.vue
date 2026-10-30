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
  { key: 'core', label: '内核日志', path: 'logs/core.log', hint: 'mihomo 运行日志' },
  { key: 'plugin', label: '插件日志', path: 'logs/clashv.log', hint: 'clashv 插件自身日志' },
]

// 副标题与悬浮说明：副标题只留体积一行，路径/描述/截断说明悬浮展示
const subTitle = computed(() => {
  const t = TABS.find(k => k.key === tab.value) || {}
  const parts = [`${t.hint}（${t.path}）`]
  if (exists.value) parts.push(`共 ${fmtSize(size.value)}`)
  if (truncated.value) parts.push('仅显示末尾 128 KB')
  return parts.join(' · ')
})

const tab = ref('core')
const raw = ref('')
const exists = ref(true)
const truncated = ref(false)
const size = ref(0)
const auto = ref(true)
const follow = ref(true)
const loading = ref(false)

// 全屏查看：日志卡固定铺满视口（筛选条一起带走），Esc 或再点一次退出
const full = ref(false)// 日志卡元素：系统级全屏的挂载目标（n-card ref 是组件实例，取 $el；普通元素直用）
const cardRef = ref(null)
const cardEl = () => cardRef.value?.$el || cardRef.value

// 全屏时锁住背景滚动（手机端触摸拖动不会带动底下的页面），退出/销毁时恢复
watch(full, v => { document.body.style.overflow = v ? 'hidden' : '' })

// 系统全屏状态同步：用户按 Esc / 浏览器退出全屏时事件会把 full 拉回 false，
// 铺满兜底态（无系统全屏事件）不受影响
function onFsChange() {
  full.value = document.fullscreenElement === cardEl()
}

// 全屏切换：优先系统级 Fullscreen API（可盖满整个浏览器窗口，LuCI 内嵌时需
// 入口页 app.js 给 iframe 放行 allowfullscreen）；未授权/不支持（requestFullscreen
// 抛错）时回退 iframe 内 fixed 铺满——只能在 iframe 区域内全屏，但功能不丢
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

// Esc 退出全屏（仅全屏态响应，不影响其他页面）
function onGlobalKey(e) {
  if (e.key === 'Escape' && full.value && !document.fullscreenElement) full.value = false
}

// 筛选条件：关键字 / 级别 / 时间范围（[起, 止] 毫秒时间戳，null=不限）
const kw = ref('')
const levelSel = ref(null)
// 时间范围拆成「开始/结束」两个独立 datetime 选择器（datetimerange 的双月历面板
// 固定 581px，手机端横向溢出；单月历面板 ~322px 放得下）。range 数据形态不变，
// 经 rangeStart/rangeEnd 双向映射并在 setter 里钳制先后，保持 range[0] <= range[1]
const range = ref(null)

// 取当天 0 点：日期可选性只按「日」粒度比较，同日内的先后交给 setter 钳制
function dayStart(ts) {
  const d = new Date(ts)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

const rangeStart = computed({
  get: () => (range.value ? range.value[0] : null),
  set(v) {
    if (v == null) { range.value = null; return } // 清空任一侧 = 清空整个范围
    const end = range.value?.[1]
    range.value = [v, end != null && end < v ? v : (end ?? v)]
  },
})
const rangeEnd = computed({
  get: () => (range.value ? range.value[1] : null),
  set(v) {
    if (v == null) { range.value = null; return }
    const start = range.value?.[0]
    range.value = [start != null && start > v ? v : (start ?? v), v]
  },
})
// 日期动态限定：开始不可选晚于结束所在日，结束不可选早于开始所在日
const disableStartDate = ts => range.value?.[1] != null && dayStart(ts) > dayStart(range.value[1])
const disableEndDate = ts => range.value?.[0] != null && dayStart(ts) < dayStart(range.value[0])

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

onMounted(() => {
  load(false)
  setAuto(true)
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
  <div class="page logs-page">
    <n-card>
      <div class="head">
        <!-- 运行日志状态：标题右侧同行小字副标题，只留路径与体积一行；完整信息悬浮可见 -->
        <div class="title-line">
          <h2 class="page-title">日志</h2>
          <span class="sub-note mono" :title="subTitle">{{ exists ? fmtSize(size) : '无文件' }}</span>
        </div>
        <!-- 标签与刷新选项独立一行 -->
        <div class="ctl-row">
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
      </div>
    </n-card>

    <n-card ref="cardRef" class="log-card" :class="{ full }">
      <!-- 筛选条：只留筛选控件，行数与操作钮挪进日志区浮动 -->
      <div class="log-bar">
        <n-input v-model:value="kw" size="small" clearable placeholder="关键字筛选…" class="f-kw">
          <template #prefix>
            <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
            </svg>
          </template>
        </n-input>
        <n-select v-model:value="levelSel" size="small" :options="LEVEL_OPTS" placeholder="筛选级别" class="f-level" />
        <!-- 时间范围拆成开始/结束两个选择器：单月历面板窄屏放得下，日期互相限定 -->
        <n-date-picker
          v-model:value="rangeStart" type="datetime" size="small" clearable
          format="yyyy-MM-dd HH:mm" placeholder="开始时间" class="f-range"
          :is-date-disabled="disableStartDate"
        />
        <n-date-picker
          v-model:value="rangeEnd" type="datetime" size="small" clearable
          format="yyyy-MM-dd HH:mm" placeholder="结束时间" class="f-range"
          :is-date-disabled="disableEndDate"
        />
      </div>

      <!-- 日志区：行数固定左上角、操作钮右侧垂直居中浮动（不随内容滚动） -->
      <div class="log-body">
        <span class="lg-count mono">{{ filtered.length }}/{{ parsed.length }} 行</span>
        <div class="float-ops">
          <!-- 全屏查看：优先系统级全屏（盖满整个浏览器窗口，LuCI 内嵌需入口页放行），
               不可用时回退 iframe 内 fixed 铺满 -->
          <n-button size="small" circle :title="full ? '退出全屏（Esc）' : '全屏查看日志'" @click="toggleFull">
            <template #icon><AppIcon :name="full ? 'shrink' : 'expand'" :size="13" /></template>
          </n-button>
          <n-button size="small" circle title="瞬移到顶部" @click="jump(true)"><template #icon><AppIcon name="chevron-up" :size="14" /></template></n-button>
          <n-button size="small" circle title="瞬移到底部" @click="jump(false)"><template #icon><AppIcon name="chevron-down" :size="14" /></template></n-button>
          <n-button size="small" circle type="error" ghost title="清空日志" @click="clearLog">
            <template #icon><AppIcon name="trash" :size="13" /></template>
          </n-button>
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
      </div>
    </n-card>
  </div>
</template>

<style scoped>
/* 标题与状态副文案同行（小字灰调），下方一行放标签+刷新选项 */
.head { margin-bottom: 12px; }
.title-line { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.sub-note { font-size: 11.5px; color: var(--text-dim); }
.ctl-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.tabs { width: 260px; flex: none; }
/* wrap + 复选框文字 nowrap：窄屏放不下时整组换行，防止「自动刷新/跟随滚动」
   被压缩成一字一行竖排（与连接页 .actions 同款防护） */
.opts { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
.opts :deep(.n-checkbox__label) { white-space: nowrap; }
/* 日志页整体精确占满内容区（.content 有 overflow:auto，若页面超高会同时出现
   内外两根滚动条）：page 高度锁死为可视区，日志卡片吃掉剩余高度，滚动只发生在
   日志组件内部；头部已包进 n-card，块间呼吸恢复走全局 16px gap（头部卡片与
   日志卡片之间的间距），不再依赖 head-row 自身的 margin-bottom */
.page.logs-page { height: 100%; gap: 16px; }
.log-card { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.log-card :deep(.n-card-content) { padding: 0; flex: 1; min-height: 0; display: flex; flex-direction: column; }

/* 筛选/操作条 */
.log-bar {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 10px 12px; border-bottom: 1px solid var(--border);
}
.f-kw { flex: 1 1 150px; max-width: 240px; }
.f-level { width: 112px; flex: none; }
/* 开始/结束两个 datetime 选择器：基线 150px 自适应等分，窄屏放不下自然换行成两行，
   宽屏各封顶 190px 不无限拉伸 */
.f-range { flex: 1 1 150px; min-width: 0; max-width: 190px; }
/* 手机端换行后铺满整行（190px 封顶会留右侧空白）；置于上方桌面规则之后保证覆盖生效 */
@media (max-width: 760px) {
  .f-range { max-width: none; }
}

/* 日志区容器：行数与操作钮浮动其上，不随内容滚动 */
.log-body { position: relative; flex: 1; min-height: 0; display: flex; flex-direction: column; }
/* 行数固定左上角：实底小胶囊盖在日志首行上方（日志区预留了顶部内边距） */
.lg-count {
  position: absolute; top: 8px; left: 12px; z-index: 2;
  color: var(--text-dim); font-size: 11px;
  background: var(--bg-card-2); border: 1px solid var(--border);
  padding: 2px 8px; border-radius: 8px;
}
/* 操作钮右侧垂直居中竖排：全屏/顶部/底部/清空（纯图标） */
.float-ops {
  position: absolute; right: 10px; top: 50%; transform: translateY(-50%); z-index: 2;
  display: flex; flex-direction: column; gap: 8px;
}

/* 全屏查看：日志卡固定铺满视口（筛选条一起带走）。实心底盖掉磨砂半透明；
   手机端 56vh 定高被压过恢复内部弹性（选择器多两个类，特异性必然高于上面媒体查询） */
.log-card.full {
  position: fixed; top: 0; right: 0; bottom: 0; left: 0; z-index: 1000;
  border-radius: 0; border: none;
  background: var(--bg);
}
.log-card.full .log-body, .log-card.full .log-view, .log-card.full .log-empty { height: auto; flex: 1; }

.log-view {
  flex: 1; box-sizing: border-box;
  /* 最小高度兜底：视口过矮时头部卡+筛选条会把 flex:1 的日志区压缩到 0（完全看不见
     日志），260px 起步保证始终可读；垫高后整页超出时由 .content 滚动兜底 */
  min-height: 260px;
  /* 顶部让位左上角行数胶囊、右侧让位浮动操作钮列 */
  padding: 32px 46px 12px 16px;
  font-family: var(--mono, ui-monospace, monospace);
  font-size: 12px; line-height: 1.75;
  overflow-y: auto;
}
.log-empty { flex: 1; min-height: 260px; overflow-y: auto; }

/* 手机端不再锁死一屏：头部卡与筛选条换行后会把 flex:1 的日志区压到只剩一条缝，
   改页面自然高度 + 日志区定高 56vh，页面整体滚动、日志组件内部滚动不变。
   必须放在桌面 .log-view/.log-empty 之后：同特异性下靠后者胜（放在前面的媒体块
   里会被上方 flex:1 盖掉，56vh 定高不生效） */
@media (max-width: 760px) {
  .page.logs-page { height: auto; }
  .log-card { flex: none; }
  .log-view, .log-empty { height: 56vh; flex: none; }
}

/* 手机端头部刷新选项：checkbox 字号压小 + 文本不换行（默认 14px 且容器窄时
   「自动刷新/跟随滚动」标签会折行），间距同步收紧；须放在上方桌面规则之后 */
@media (max-width: 760px) {
  .opts { gap: 8px; }
  .opts :deep(.n-checkbox) { font-size: 12px; }
  .opts :deep(.n-checkbox__label) { white-space: nowrap; padding-left: 6px; }
}
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
