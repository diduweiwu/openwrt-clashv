<script setup>
// 首页：运行状态、当前订阅、流量概览、快速切换节点
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api.js'
import { store, toast, fmtRate, fmtBytes, fmtUptime, pushTraffic } from '../store.js'
import Sparkline from '../components/Sparkline.vue'
import NodeSheet from '../components/NodeSheet.vue'

const router = useRouter()
const busy = ref('')
const proxies = ref({})
const showSheet = ref(false)
const sheetGroup = ref(null)
const activeProfile = ref(null) // 当前激活订阅的完整信息（含流量）
const subBusy = ref(false)
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

// ---- 当前订阅 ----
async function loadProfiles() {
  try {
    const data = await api.get('/api/profiles')
    const list = data.profiles || []
    activeProfile.value = list.find(p => p.id === data.active) || null
  } catch { /* 忽略 */ }
}

async function refreshProfile() {
  if (!activeProfile.value) return
  subBusy.value = true
  try {
    const r = await api.post(`/api/profiles/${activeProfile.value.id}/update`)
    toast('订阅已更新' + (r.restarted ? '，内核已重载' : ''), 'success')
    loadProfiles()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    subBusy.value = false
  }
}

// 订阅流量：已用/总量（机场未提供 total 时不显示）
const subTraffic = computed(() => {
  const p = activeProfile.value
  if (!p?.total) return null
  const used = (p.upload || 0) + (p.download || 0)
  return { used, total: p.total, percent: Math.min(100, (used / p.total) * 100) }
})

// ---- 切换订阅弹窗 ----
const showSwitch = ref(false)
const switchList = ref([])
const switchActive = ref('')
const switchKeyword = ref('')
const switchSelected = ref('')
const switchLoading = ref(false)
const switching = ref(false)

async function openSwitch() {
  showSwitch.value = true
  switchLoading.value = true
  switchKeyword.value = ''
  switchSelected.value = ''
  try {
    const data = await api.get('/api/profiles')
    switchList.value = data.profiles || []
    switchActive.value = data.active || ''
    switchSelected.value = switchActive.value
  } catch (e) {
    showSwitch.value = false
    toast(e.message, 'error')
  } finally {
    switchLoading.value = false
  }
}

// 名称模糊匹配：忽略大小写与空格的子序列匹配（如 hk 命中「香港-01」）
function fuzzyHit(name, kw) {
  const k = (kw || '').toLowerCase().replace(/\s+/g, '')
  if (!k) return true
  let i = 0
  for (const ch of name.toLowerCase()) {
    if (ch === k[i]) i++
    if (i >= k.length) return true
  }
  return false
}

const switchFiltered = computed(() =>
  switchList.value.filter(p => fuzzyHit(p.name, switchKeyword.value))
)

async function confirmSwitch() {
  if (!switchSelected.value || switching.value) return
  switching.value = true
  try {
    const r = await api.post(`/api/profiles/${switchSelected.value}/activate`)
    if (r.error) {
      toast('切换失败：' + r.error, 'error')
    } else {
      toast('订阅已切换' + (r.restarted ? '，内核已重载' : ''), 'success')
      showSwitch.value = false
      loadProfiles()
      refreshStatus()
      loadProxies()
    }
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    switching.value = false
  }
}
const subExpire = computed(() => {
  if (!activeProfile.value?.expire) return ''
  const d = new Date(activeProfile.value.expire * 1000)
  const p2 = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`
})

// ---- 运行时配置查看 ----
const showConfig = ref(false)
const cfgContent = ref('')
const cfgLoading = ref(false)

async function viewConfig() {
  showConfig.value = true
  cfgLoading.value = true
  try {
    const r = await api.get('/api/core/config')
    cfgContent.value = r.content || ''
  } catch (e) {
    showConfig.value = false
    toast(e.message, 'error')
  } finally {
    cfgLoading.value = false
  }
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
  loadProfiles()
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
              <span v-else-if="status?.openwrt" class="badge">透明代理</span>
              <span v-else class="v">标准</span>
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

    <!-- 当前订阅 -->
    <div class="card">
      <div class="sec-head">
        <h3>当前订阅</h3>
        <div class="sub-actions">
          <button class="ghost sm" :disabled="subBusy || !activeProfile" @click="refreshProfile">
            {{ subBusy ? '更新中…' : '刷新订阅' }}
          </button>
          <button v-if="status?.running" class="ghost sm" @click="viewConfig">运行时配置</button>
          <button class="ghost sm" @click="openSwitch">切换订阅</button>
          <button class="ghost sm" @click="router.push('/profiles?add=1')">添加订阅</button>
        </div>
      </div>
      <div v-if="!status?.profile" class="empty-hint">未设置订阅，请先添加并启用</div>
      <template v-else>
        <div class="sub-row">
          <span class="sub-name">{{ status.profile }}</span>
          <span v-if="subExpire" class="page-sub">到期 {{ subExpire }}</span>
        </div>
        <div v-if="subTraffic" class="sub-traffic">
          <div class="sub-bar"><div class="sub-bar-fill" :style="{ width: subTraffic.percent + '%' }"></div></div>
          <span class="mono sub-traffic-text">
            已用 {{ fmtBytes(subTraffic.used) }} / {{ fmtBytes(subTraffic.total) }}（{{ Math.round(subTraffic.percent) }}%）
          </span>
        </div>
        <div v-else class="page-sub" style="margin-top:4px">机场未提供流量信息</div>
      </template>
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

    <!-- 切换订阅弹窗 -->
    <div v-if="showSwitch" class="overlay" @click.self="showSwitch = false">
      <div class="switch-modal">
        <div class="cfg-head">
          <h3>切换订阅</h3>
          <button class="ghost sm" @click="showSwitch = false">关闭</button>
        </div>
        <input
          v-model="switchKeyword"
          class="switch-search"
          type="text"
          placeholder="按名称搜索订阅，支持关键字模糊匹配…"
          spellcheck="false"
        >
        <div class="switch-list">
          <div v-if="switchLoading" class="empty-hint">加载中…</div>
          <div v-else-if="!switchFiltered.length" class="empty-hint">
            {{ switchList.length ? '没有匹配的订阅' : '还没有订阅，请先到「订阅」页添加' }}
          </div>
          <button
            v-for="p in switchFiltered"
            :key="p.id"
            class="switch-row"
            :class="{ sel: switchSelected === p.id, cur: switchActive === p.id }"
            @click="switchSelected = p.id"
          >
            <span class="s-name">{{ p.name }}</span>
            <span v-if="switchActive === p.id" class="badge">当前</span>
            <span v-if="switchSelected === p.id" class="s-check">✓</span>
          </button>
        </div>
        <div class="switch-foot">
          <span class="page-sub">选中后需确认才会切换并重载内核</span>
          <button
            class="primary"
            :disabled="!switchSelected || switching || switchSelected === switchActive"
            @click="confirmSwitch"
          >{{ switching ? '切换中…' : '确认切换' }}</button>
        </div>
      </div>
    </div>

    <!-- 运行时配置查看 -->
    <div v-if="showConfig" class="overlay" @click.self="showConfig = false">
      <div class="cfg-modal">
        <div class="cfg-head">
          <h3>运行时配置（config.yaml）</h3>
          <button class="ghost sm" @click="showConfig = false">关闭</button>
        </div>
        <pre v-if="!cfgLoading" class="cfg-view mono">{{ cfgContent }}</pre>
        <div v-else class="empty-hint" style="padding:40px">加载中…</div>
      </div>
    </div>
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
.sub-actions { display: flex; gap: 8px; }
.sub-row { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.sub-name { font-size: 15px; font-weight: 600; }
.sub-traffic { display: flex; align-items: center; gap: 12px; margin-top: 9px; }
.sub-bar { width: 260px; max-width: 50%; height: 6px; border-radius: 3px; background: var(--border); overflow: hidden; }
.sub-bar-fill { height: 100%; border-radius: 3px; background: var(--accent); transition: width 0.4s ease; }
.sub-traffic-text { color: var(--text-dim); font-size: 12.5px; }
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

.overlay {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0, 0, 0, 0.55);
  display: flex; align-items: center; justify-content: center;
  padding: 20px;
}
.cfg-modal {
  width: 760px; max-width: 100%; height: 80vh;
  display: flex; flex-direction: column;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: var(--shadow);
  padding: 18px;
}
.cfg-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.cfg-head h3 { font-size: 14.5px; }
.cfg-view {
  flex: 1; margin: 0; padding: 14px;
  overflow: auto;
  background: var(--bg-card-2);
  border: 1px solid var(--border); border-radius: 10px;
  font-size: 12px; line-height: 1.6;
  white-space: pre; tab-size: 2;
}
.switch-modal {
  width: 520px; max-width: 100%; max-height: 78vh;
  display: flex; flex-direction: column;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: var(--shadow);
  padding: 18px;
}
.switch-search { width: 100%; margin-bottom: 12px; flex: none; }
.switch-list {
  flex: 1; min-height: 120px; overflow-y: auto;
  display: flex; flex-direction: column; gap: 6px;
}
.switch-row {
  display: flex; align-items: center; gap: 10px;
  width: 100%; text-align: left;
  padding: 11px 14px; border-radius: 11px;
  background: var(--bg-card-2);
  border: 1.5px solid transparent;
}
.switch-row:hover { filter: none; background: var(--hover); }
.switch-row.sel { border-color: var(--accent); background: var(--accent-soft); }
.switch-row.cur .s-name { color: var(--accent); }
.s-name {
  flex: 1; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-weight: 600; font-size: 13.5px;
}
.s-check { color: var(--accent); font-weight: 700; }
.switch-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  margin-top: 14px; flex: none;
}
</style>
