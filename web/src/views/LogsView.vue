<script setup>
// 日志页：内核日志 + 插件日志（连接明细已独立为「连接」页）
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { NButton, NCard, NCheckbox, NEmpty, NTabs, NTabPane } from 'naive-ui'
import { api } from '../api.js'
import { toast } from '../store.js'

const TABS = [
  { key: 'core', label: '内核日志', hint: 'mihomo 运行日志（logs/core.log）' },
  { key: 'plugin', label: '插件日志', hint: 'clashv 插件自身日志（logs/clashv.log）' },
]

const tab = ref('core')
const content = ref('')
const exists = ref(true)
const truncated = ref(false)
const size = ref(0)
const auto = ref(true)
const follow = ref(true)
const loading = ref(false)

const preRef = ref(null)
let timer = null

function fmtSize(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

// 行首时间戳：内核日志 time="2026-…Z"（logrus 带引号）、插件日志 time=2026-…+08:00（slog 无引号）
const TIME_RE = /^time=(?:"([^"]+)"|(\S+))/

// 归一化展示：把行首 time= 统一为本地「年-月-日 时:分:秒.毫秒」。
// 内核若还在写 UTC（Z 结尾），浏览器会换算成本地时区，时区错乱在界面上不可见
function normalizeLog(text) {
  if (!text) return ''
  const p = (n, l = 2) => String(n).padStart(l, '0')
  return text.split('\n').map(line => {
    const m = TIME_RE.exec(line)
    if (!m) return line
    // RFC3339 带纳秒（9 位小数）时截到毫秒，超出 JS Date 解析精度
    const raw = (m[1] || m[2] || '').replace(/\.(\d{3})\d+/, '.$1')
    const d = new Date(raw)
    if (isNaN(d)) return line
    const ts = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
      `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
    return ts + line.slice(m[0].length)
  }).join('\n')
}

async function load(silent = true) {
  loading.value = !silent
  try {
    const r = await api.get(`/api/logs?kind=${tab.value}&bytes=131072`)
    const hadEnd =
      !preRef.value || preRef.value.scrollTop + preRef.value.clientHeight >= preRef.value.scrollHeight - 30
    content.value = normalizeLog(r.content || '')
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
  content.value = ''
  load(false)
}

onMounted(() => { load(false); setAuto(true) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="page logs-page">
    <div class="head-row">
      <n-tabs :value="tab" type="segment" size="small" class="tabs" @update:value="pick">
        <n-tab-pane v-for="k in TABS" :key="k.key" :name="k.key">
          <template #tab>{{ k.label }}</template>
        </n-tab-pane>
      </n-tabs>
      <div class="opts">
        <n-checkbox v-model:checked="auto" @update:checked="setAuto">自动刷新</n-checkbox>
        <n-checkbox v-model:checked="follow">跟随滚动</n-checkbox>
        <n-button size="small" :loading="loading" @click="load(false)">刷新</n-button>
      </div>
    </div>

    <p class="page-sub meta-line">
      {{ TABS.find(k => k.key === tab)?.hint }} ·
      {{ exists ? `共 ${fmtSize(size)}` : '暂无日志' }}<template v-if="truncated">（仅显示末尾 128 KB）</template>
    </p>

    <n-card class="log-card">
      <pre v-if="exists && content" ref="preRef" class="log-view">{{ content }}</pre>
      <n-empty v-else-if="exists" description="日志为空，启动内核后这里会有输出" style="padding: 60px 0" />
      <n-empty v-else description="暂无日志文件" style="padding: 60px 0" />
    </n-card>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.tabs { width: 260px; flex: none; }
.opts { display: flex; align-items: center; gap: 14px; }
.meta-line { margin: 8px 2px 10px; }
/* 日志页整体精确占满内容区（.content 有 overflow:auto，若页面超高会同时出现
   内外两根滚动条）：page 高度锁死为可视区，卡片吃掉剩余高度，滚动只发生在
   日志组件内部 */
.logs-page { height: 100%; }
.log-card { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.log-card :deep(.n-card-content) { padding: 0; flex: 1; min-height: 0; display: flex; flex-direction: column; }
.log-view {
  flex: 1; min-height: 0; box-sizing: border-box;
  margin: 0; padding: 14px 16px;
  font-family: var(--mono, ui-monospace, monospace);
  font-size: 12px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-all;
  overflow-y: auto;
}
</style>
