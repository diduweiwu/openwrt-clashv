<script setup>
// 日志页：内核日志 + 插件日志（连接明细已独立为「连接」页）
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
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
        <label class="opt"><input v-model="follow" type="checkbox"> 跟随滚动</label>
        <button class="ghost sm" :disabled="loading" @click="load(false)">刷新</button>
      </div>
    </div>

    <p class="page-sub meta-line">
      {{ TABS.find(k => k.key === tab)?.hint }} ·
      {{ exists ? `共 ${fmtSize(size)}` : '暂无日志' }}<template v-if="truncated">（仅显示末尾 128 KB）</template>
    </p>

    <div class="card log-card">
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
</style>
