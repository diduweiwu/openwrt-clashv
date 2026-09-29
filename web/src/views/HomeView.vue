<script setup>
// 首页：运行状态、流量概览、快速切换节点
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '../api.js'
import { store, toast, fmtRate, fmtBytes, fmtUptime } from '../store.js'
import Sparkline from '../components/Sparkline.vue'
import NodeSheet from '../components/NodeSheet.vue'

const busy = ref('')
const proxies = ref({})
const showSheet = ref(false)
const sheetGroup = ref(null)
let trafficTimer = null

const GROUP_TYPES = ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay']

const groups = computed(() =>
  Object.values(proxies.value)
    .filter(p => GROUP_TYPES.includes(p.type) && Array.isArray(p.all) && p.all.length && p.name !== 'GLOBAL')
    .map(p => ({ name: p.name, type: p.type, now: p.now || '', all: p.all }))
)

const selectableGroups = computed(() => groups.value.filter(g => g.type === 'Selector'))

const status = computed(() => store.status)
const traffic = computed(() => store.traffic)

async function loadProxies(silent = true) {
  if (!status.value?.running) { proxies.value = {}; return }
  try {
    const data = await api.get('/api/proxies')
    proxies.value = data.proxies || {}
  } catch (e) {
    if (!silent) toast(e.message, 'error')
  }
}

async function coreAction(action) {
  busy.value = action
  try {
    await api.post('/api/core/' + action)
    toast(action === 'stop' ? '内核已停止' : '内核已启动', 'success')
    await refreshStatus()
    if (action !== 'stop') loadProxies()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    busy.value = ''
  }
}

async function refreshStatus() {
  try { store.status = await api.get('/api/status') } catch { /* 忽略 */ }
}

function openGroup(g) {
  sheetGroup.value = g
  showSheet.value = true
}

// status 晚于挂载到达时，运行起来后补一次代理列表
watch(
  () => store.status?.running,
  (running, prev) => {
    if (running && !prev) loadProxies()
  },
)

// 流量轮询只在首页进行（离开页面即停止，减少无谓请求与重绘）
async function refreshTraffic() {
  if (!store.status?.running) return
  try {
    pushTraffic(await api.get('/api/traffic'))
  } catch { /* 忽略单次失败 */ }
}

onMounted(() => {
  loadProxies()
  refreshTraffic()
  trafficTimer = setInterval(refreshTraffic, 1000)
})
onUnmounted(() => clearInterval(trafficTimer))

function currentOf(g) {
  const p = proxies.value[g.name]
  return p?.now || '—'
}
</script>

<template>
  <div class="page">
    <!-- 运行状态 -->
    <div class="card hero">
      <div class="hero-main">
        <div class="run-badge" :class="{ on: status?.running }">
          <span class="pulse"></span>
          <span class="run-text">{{ status?.running ? '运行中' : '已停止' }}</span>
        </div>
        <div class="meta">
          <div class="meta-item">
            <span class="k">当前订阅</span>
            <span class="v">{{ status?.profile || '未设置' }}</span>
          </div>
          <div class="meta-item">
            <span class="k">内核版本</span>
            <span class="v mono">{{ status?.core?.version || '未安装' }}</span>
          </div>
          <div class="meta-item">
            <span class="k">运行时长</span>
            <span class="v mono">{{ status?.running ? fmtUptime(status.uptime) : '—' }}</span>
          </div>
          <div class="meta-item">
            <span class="k">混合端口</span>
            <span class="v mono">{{ status?.mixed_port || '—' }}</span>
          </div>
          <div class="meta-item">
            <span class="k">模式</span>
            <span class="v">
              <span v-if="status?.tun" class="badge">TUN</span>
              <span v-if="status?.allow_lan" class="badge" style="margin-left:4px">局域网</span>
              <span v-if="!status?.tun && !status?.allow_lan" class="v">标准</span>
            </span>
          </div>
        </div>
      </div>
      <div class="actions">
        <button v-if="!status?.running" class="primary" :disabled="busy !== ''" @click="coreAction('start')">
          {{ busy === 'start' ? '启动中…' : '▶ 启动内核' }}
        </button>
        <template v-else>
          <button class="ghost" :disabled="busy !== ''" @click="coreAction('restart')">
            {{ busy === 'restart' ? '重启中…' : '重启' }}
          </button>
          <button class="danger" :disabled="busy !== ''" @click="coreAction('stop')">
            {{ busy === 'stop' ? '停止中…' : '■ 停止' }}
          </button>
        </template>
      </div>
    </div>

    <!-- 流量 -->
    <div class="card">
      <div class="traffic-head">
        <h3>实时流量</h3>
        <div class="traffic-nums">
          <div class="tn">
            <span class="k">↑ 上传</span>
            <span class="v mono" style="color: var(--green)">{{ fmtRate(traffic.up) }}</span>
          </div>
          <div class="tn">
            <span class="k">↓ 下载</span>
            <span class="v mono" style="color: var(--accent)">{{ fmtRate(traffic.down) }}</span>
          </div>
          <div class="tn">
            <span class="k">连接</span>
            <span class="v mono">{{ traffic.connections }}</span>
          </div>
          <div class="tn">
            <span class="k">内核内存</span>
            <span class="v mono">{{ traffic.memory_mb ? traffic.memory_mb.toFixed(1) + ' MB' : '—' }}</span>
          </div>
        </div>
      </div>
      <Sparkline v-if="status?.running" :series="store.history" :height="130" />
      <div v-else class="traffic-empty">内核未运行</div>
    </div>

    <!-- 快速切换 -->
    <div class="card">
      <div class="sec-head">
        <h3>快速切换节点</h3>
        <span class="page-sub">点击分组选择节点 · 仅显示可手动选择分组</span>
      </div>
      <div v-if="!status?.running" class="empty-hint">内核未运行，启动后可切换节点</div>
      <div v-else-if="selectableGroups.length === 0" class="empty-hint">订阅中没有可手动选择的代理组</div>
      <div v-else class="group-list">
        <button v-for="g in selectableGroups" :key="g.name" class="group-row" @click="openGroup(g)">
          <span class="g-name">{{ g.name }}</span>
          <span class="g-count">{{ g.all.length }} 节点</span>
          <span class="g-now">{{ currentOf(g) }}</span>
          <span class="g-arrow">›</span>
        </button>
      </div>
    </div>

    <NodeSheet
      v-if="showSheet"
      :group="sheetGroup"
      :proxies="proxies"
      @close="showSheet = false"
      @selected="loadProxies"
    />
  </div>
</template>

<style scoped>
.hero { display: flex; align-items: center; justify-content: space-between; gap: 18px; flex-wrap: wrap; }
.hero-main { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.run-badge { display: flex; align-items: center; gap: 10px; }
.run-text { font-size: 21px; font-weight: 700; }
.pulse { width: 12px; height: 12px; border-radius: 50%; background: #5a5f6d; position: relative; }
.pulse::after { content: ''; position: absolute; inset: -5px; border-radius: 50%; border: 2px solid transparent; }
.run-badge.on .pulse { background: var(--green); }
.run-badge.on .pulse::after { border-color: var(--green); animation: ring 1.8s ease-out infinite; }
@keyframes ring {
  0% { transform: scale(0.5); opacity: 1; }
  100% { transform: scale(1.5); opacity: 0; }
}
.meta { display: flex; gap: 30px; flex-wrap: wrap; }
.meta-item { display: flex; flex-direction: column; gap: 3px; }
.meta-item .k { color: var(--text-dim); font-size: 12px; }
.meta-item .v { font-size: 13.5px; font-weight: 500; }
.actions { display: flex; gap: 10px; }

.traffic-head { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: 10px; margin-bottom: 8px; }
.traffic-nums { display: flex; gap: 26px; flex-wrap: wrap; }
.tn { display: flex; flex-direction: column; gap: 2px; }
.tn .k { color: var(--text-dim); font-size: 12px; }
.tn .v { font-size: 16px; font-weight: 600; }
.traffic-empty {
  height: 130px; display: flex; align-items: center; justify-content: center;
  color: var(--text-dim); border: 1px dashed var(--border); border-radius: 10px;
}

.sec-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.group-list { display: flex; flex-direction: column; gap: 6px; }
.group-row {
  display: flex; align-items: center; gap: 12px;
  width: 100%; text-align: left;
  padding: 12px 14px; border-radius: 11px;
  background: var(--bg-card-2);
}
.g-name { font-weight: 600; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.g-count { color: var(--text-dim); font-size: 12px; flex: none; }
.g-now {
  color: var(--accent); font-size: 13px; max-width: 40%;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.g-arrow { color: var(--text-dim); font-size: 18px; flex: none; }
.empty-hint { color: var(--text-dim); text-align: center; padding: 22px 0; }
</style>
