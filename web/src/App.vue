<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { NConfigProvider, NModal, NProgress, NSpin } from 'naive-ui'
import Sidebar from './components/Sidebar.vue'
import { store, naiveTheme, naiveOverrides, toast, ask, fmtBytes, pushTraffic } from './store.js'
import { api } from './api.js'

let statusTimer = null
let trafficTimer = null
let lastStarting = null

async function refreshStatus() {
  try {
    store.status = await api.get('/api/status')
    // 启动窗口期把轮询加密到 1s，让「启动中…→运行中」的切换即时可见
    const starting = !!store.status?.starting
    if (starting !== lastStarting) {
      lastStarting = starting
      armPolling()
    }
  } catch (e) {
    // 静默，避免后端未启动时刷屏
  }
}

function armPolling() {
  if (statusTimer) clearInterval(statusTimer)
  statusTimer = setInterval(refreshStatus, store.status?.starting ? 1000 : 5000)
}

// 流量每秒轮询（全局）：驱动首页流量曲线与侧栏底部的连接/内存小卡
async function refreshTraffic() {
  if (!store.status?.running) return
  try {
    pushTraffic(await api.get('/api/traffic'))
  } catch { /* 忽略单次失败 */ }
}

onMounted(() => {
  refreshStatus()
  armPolling()
  trafficTimer = setInterval(refreshTraffic, 1000)
})
onUnmounted(() => {
  clearInterval(statusTimer)
  clearInterval(trafficTimer)
  stopDlProgPoll()
})

// ---- 内核缺失检测：发现未安装内核时弹窗询问是否下载 ----
let coreAsked = false // 每次页面会话只问一次，取消后不再自动弹
watch(
  () => store.status?.core?.installed,
  (installed) => {
    if (installed === false && !coreAsked) {
      coreAsked = true
      askCoreDownload()
    }
  },
)

async function askCoreDownload() {
  const ok = await ask(
    '未检测到内核',
    '内核文件不存在，无法启动代理。是否立即从 GitHub 下载并安装最新版 mihomo 内核？也可以稍后在「设置 → 内核」中手动下载。',
  )
  if (ok) startCoreDownload()
}

// 下载弹窗：轮询 /api/upgrade/progress 展示进度，完成后刷新状态
const showDl = ref(false)
const dlBusy = ref(false)
const dlProg = ref(null)
let dlProgTimer = null

const dlProgText = computed(() => {
  const p = dlProg.value
  if (!p) return ''
  const parts = [p.message]
  if (p.total > 0) parts.push(`${fmtBytes(p.downloaded)} / ${fmtBytes(p.total)}（${Math.round(p.percent)}%）`)
  else if (p.downloaded > 0) parts.push(fmtBytes(p.downloaded))
  return parts.filter(Boolean).join('，')
})

function startDlProgPoll() {
  stopDlProgPoll()
  dlProg.value = null
  const tick = async () => {
    try { dlProg.value = await api.get('/api/upgrade/progress') } catch { /* 轮询失败下次再试 */ }
  }
  tick()
  dlProgTimer = setInterval(tick, 600)
}
function stopDlProgPoll() {
  if (dlProgTimer) { clearInterval(dlProgTimer); dlProgTimer = null }
}

async function startCoreDownload() {
  showDl.value = true
  dlBusy.value = true
  startDlProgPoll()
  try {
    await api.post('/api/core/upgrade')
    toast('内核下载安装完成', 'success', 5000)
    showDl.value = false
    refreshStatus()
  } catch (e) {
    toast(e.message, 'error', 6000)
    showDl.value = false
  } finally {
    stopDlProgPoll()
    dlBusy.value = false
  }
}
</script>

<template>
  <n-config-provider class="provider" :theme="naiveTheme" :theme-overrides="naiveOverrides">
    <div class="layout">
      <Sidebar />
      <main class="content">
        <router-view v-slot="{ Component }">
          <transition name="page" mode="out-in">
            <component :is="Component" :key="$route.fullPath" />
          </transition>
        </router-view>
      </main>
    </div>

    <!-- 内核下载进度弹窗（下载中不可关闭，关页面不会中断后端下载但不建议） -->
    <n-modal
      preset="card"
      title="下载内核"
      :show="showDl"
      :closable="!dlBusy"
      :mask-closable="!dlBusy"
      :style="{ width: '480px', maxWidth: '94vw' }"
      @update:show="showDl = false"
    >
      <div class="dl-body">
        <div class="dl-text mono">{{ dlProgText || '正在连接…' }}</div>
        <n-progress
          v-if="dlProg?.percent > 0"
          type="line"
          :percentage="dlProg.percent"
          :show-indicator="false"
          :height="8"
          border-radius="4px"
        />
        <n-spin v-else :size="18" />
        <div class="dl-hint">视网络情况可能需要几分钟，请保持页面打开</div>
      </div>
    </n-modal>
  </n-config-provider>
</template>

<style scoped>
/* NConfigProvider 自带一层 div，需接上 #app 的 height:100% 链，侧栏才能占满全高 */
.provider { height: 100%; }
.layout { display: flex; height: 100%; }
.content {
  flex: 1;
  overflow-y: auto;
  padding: 26px 30px 40px;
}
@media (max-width: 760px) {
  .layout { flex-direction: column; }
  .content { padding: 18px 14px 30px; }
}
.dl-body { display: flex; flex-direction: column; align-items: center; gap: 14px; padding: 6px 0 2px; }
.dl-text { font-size: 13px; text-align: center; min-height: 18px; }
.dl-hint { color: var(--text-dim); font-size: 12px; }
</style>
