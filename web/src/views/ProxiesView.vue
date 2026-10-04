<script setup>
// 代理页：全部代理组 + 节点卡片，点击切换、整组测速
import { computed, onMounted, ref, watch } from 'vue'
import { NButton, NCard, NEmpty, NTag } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, delayColor } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

const proxies = ref({})
const loading = ref(true)
const testing = ref('') // 正在测速的组名
const testProg = ref('') // 测速进度 "已完成/总数"
const switching = ref('') // 正在切换的 节点@组

const GROUP_TYPES = ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay']

const groups = computed(() =>
  Object.values(proxies.value)
    .filter(p => GROUP_TYPES.includes(p.type) && Array.isArray(p.all) && p.all.length && p.name !== 'GLOBAL')
)

function delayOf(nodeName) {
  const p = proxies.value[nodeName]
  const h = p?.history
  if (Array.isArray(h) && h.length) return h[h.length - 1].delay
  return 0
}

async function load() {
  loading.value = true
  try {
    if (!store.status?.running) { proxies.value = {}; return }
    const data = await api.get('/api/proxies')
    proxies.value = data.proxies || {}
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    loading.value = false
  }
}

async function select(groupName, nodeName) {
  const p = proxies.value[groupName]
  if (p && p.type !== 'Selector') {
    toast('该分组为自动选择，无法手动切换', 'info')
    return
  }
  switching.value = groupName + '@' + nodeName
  try {
    await api.put('/api/proxies/' + encodeURIComponent(groupName), { name: nodeName })
    if (proxies.value[groupName]) proxies.value[groupName].now = nodeName
    toast(`已切换到 ${nodeName}`, 'success')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    switching.value = ''
  }
}

async function testGroup(g) {
  testing.value = g.name
  // 路由器 CPU 弱，内核的整组测速接口会瞬间并发测所有节点导致全部超时，
  // 这里改为逐节点测速、限制并发，结果实时回填到卡片上。
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

// status 晚于挂载到达时，运行起来后自动加载一次
watch(
  () => store.status?.running,
  (running, prev) => {
    if (running && !prev && proxies.value && Object.keys(proxies.value).length === 0) load()
  },
)

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h2 class="page-title">代理</h2>
        <span class="page-sub">点击节点切换 · {{ groups.length }} 个代理组</span>
      </div>
      <n-button :loading="loading" @click="load">
        <template #icon><AppIcon name="refresh" :size="15" /></template>
        刷新
      </n-button>
    </div>

    <n-card v-if="!store.status?.running" class="pad">
      <n-empty description="内核未运行，启动后此处显示代理列表" />
    </n-card>
    <n-card v-else-if="loading" class="pad">
      <n-empty description="加载中…" />
    </n-card>
    <n-card v-else-if="groups.length === 0" class="pad">
      <n-empty description="订阅中没有代理组" />
    </n-card>

    <n-card v-for="g in groups" :key="g.name">
      <div class="g-head">
        <div class="g-title">
          <h3>{{ g.name }}</h3>
          <n-tag size="small" :bordered="false">{{ g.type }}</n-tag>
          <span v-if="g.now" class="page-sub">当前 {{ g.now }}</span>
        </div>
        <n-button size="small" :loading="testing === g.name" :disabled="testing !== ''" @click="testGroup(g)">
          <template #icon><AppIcon name="zap" :size="13" /></template>
          {{ testing === g.name ? `测速中 ${testProg}` : '整组测速' }}
        </n-button>
      </div>
      <div class="nodes">
        <button
          v-for="node in g.all"
          :key="g.name + node"
          class="node-card"
          :class="{ current: node === g.now }"
          @click="select(g.name, node)"
        >
          <span class="n-name">{{ node }}</span>
          <span class="n-foot">
            <span class="n-type">{{ proxies[node]?.type || '' }}</span>
            <span class="mono n-delay" :style="{ color: delayColor(delayOf(node)) }">
              {{ delayOf(node) > 0 ? delayOf(node) + 'ms' : switching === g.name + '@' + node ? '切换中…' : '' }}
            </span>
          </span>
        </button>
      </div>
    </n-card>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; }
.pad :deep(.n-empty) { padding: 34px 0; }
.g-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 13px; flex-wrap: wrap; }
.g-title { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; min-width: 0; }
.g-title h3 { font-size: 15px; max-width: 340px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.nodes {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(158px, 1fr));
  gap: 9px;
}
.node-card {
  display: flex; flex-direction: column; gap: 7px;
  text-align: left;
  padding: 11px 12px;
  border-radius: 11px;
  background: var(--bg-card-2);
  border: 1.5px solid transparent;
  min-width: 0;
  cursor: pointer; font: inherit; color: inherit;
}
.node-card:hover { border-color: var(--accent); }
.node-card.current {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.n-name {
  font-size: 13px; font-weight: 500;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.n-foot { display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.n-type { color: var(--text-dim); font-size: 11px; text-transform: uppercase; }
.n-delay { font-size: 12px; }
</style>
