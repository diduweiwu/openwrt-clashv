<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { NButton, NConfigProvider, NModal, NProgress, NSpin, dateZhCN, zhCN } from 'naive-ui'
import Sidebar from './components/Sidebar.vue'
import SetupWizard from './components/SetupWizard.vue'
import { store, naiveTheme, naiveOverrides, toast, ask, fmtBytes, pushTraffic, setupDismissed } from './store.js'
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

// ---- 内核缺失检测 + 初始化引导自动弹出 ----
// 自动弹规则：配置未就绪（缺内核或缺激活订阅）时，打开页面自动弹引导（每会话至多
// 一次）；用户勾选「不再自动弹出」并关闭后（setupDismissed）永不再自动弹，此时内核
// 缺失回退到旧的提醒弹窗。手动入口（首页按钮）不受任何影响。
let coreAsked = false // 内核缺失提醒每会话只问一次，取消后不再自动弹
let autoWizardDone = false // 首访自动引导每会话至多弹一次

// 自动弹出初始化引导（自动模式：引导内显示「不再自动弹出」勾选）
function openAutoWizard() {
  coreAsked = true
  store.wizardAuto = true
  store.wizardOpen = true
}

// 首次拿到完整状态时判断是否自动弹（覆盖「内核已装但没订阅」的新装场景）
watch(
  () => store.status,
  (s) => {
    if (!s || autoWizardDone || store.wizardOpen) return
    autoWizardDone = true
    if (setupDismissed()) return
    if (!s.core?.installed || !s.profile) openAutoWizard()
  },
)

watch(
  () => store.status?.core?.installed,
  async (installed) => {
    if (installed === false && !coreAsked) {
      coreAsked = true
      // 初始化引导开着时由引导接管下载流程（含任务恢复），这里不再重复询问
      if (store.wizardOpen) return
      // 用户已永久关闭自动引导：保留原「任务恢复/内核缺失提醒」兜底
      if (setupDismissed()) {
        try {
          const p = await api.get('/api/upgrade/progress')
          if (p?.active && p.kind === 'core') {
            resumeCoreDownload()
            return
          }
        } catch { /* 查询失败走正常询问 */ }
        askCoreDownload()
        return
      }
      // 内核未装（首次打开页面的常见情况）：直接自动弹引导，引导内可下载内核并
      // 恢复进行中的下载任务（detect 查询 progress），无需旧确认弹窗
      openAutoWizard()
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

// 下载弹窗：轮询 /api/upgrade/progress 展示进度，完成后刷新状态。
// dlCancelled 标记取消是用户自己点的：POST /api/core/upgrade 会以「已取消」失败、
// 恢复轮询也会拿到 error 终态，两处都靠它跳过错误 toast。
const showDl = ref(false)
const dlBusy = ref(false)
const dlCancelled = ref(false)
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

// resume = 页面刷新后重连已在上跑的任务：终态在这里收尾。
// 正常发起的下载由升级接口自身的返回收尾，不走这里防重复提示。
function startDlProgPoll(resume = false) {
  stopDlProgPoll()
  dlProg.value = null
  const tick = async () => {
    try {
      dlProg.value = await api.get('/api/upgrade/progress')
    } catch { /* 轮询失败下次再试 */ return }
    if (!(resume && dlProg.value && !dlProg.value.active)) return
    const { stage, message } = dlProg.value
    stopDlProgPoll()
    dlBusy.value = false
    showDl.value = false
    if (stage === 'done') {
      toast('内核下载安装完成', 'success', 5000)
      refreshStatus()
    } else if (stage === 'error' && !dlCancelled.value) {
      toast(message || '内核下载失败', 'error', 6000)
    }
  }
  tick()
  dlProgTimer = setInterval(tick, 600)
}
function stopDlProgPoll() {
  if (dlProgTimer) { clearInterval(dlProgTimer); dlProgTimer = null }
}

// 恢复跟进进行中的内核下载：重开进度弹窗，完成后自动关闭并刷新状态
function resumeCoreDownload() {
  showDl.value = true
  dlBusy.value = true
  dlCancelled.value = false
  startDlProgPoll(true)
}

// 取消进行中的内核下载：中断后端任务，弹窗由接口失败/轮询终态收尾关闭
async function cancelCoreDownload() {
  dlCancelled.value = true
  try { await api.post('/api/upgrade/cancel') } catch { /* 任务可能刚自行结束 */ }
}

async function startCoreDownload() {
  showDl.value = true
  dlBusy.value = true
  dlCancelled.value = false
  startDlProgPoll()
  try {
    await api.post('/api/core/upgrade')
    toast('内核下载安装完成', 'success', 5000)
    showDl.value = false
    refreshStatus()
  } catch (e) {
    if (!dlCancelled.value) toast(e.message, 'error', 6000)
    showDl.value = false
  } finally {
    stopDlProgPoll()
    dlBusy.value = false
  }
}
</script>

<template>
  <!-- locale：naive 内置文案（下拉「请选择」、日期选择器起止占位、分页等）走中文 -->
  <n-config-provider class="provider" :theme="naiveTheme" :theme-overrides="naiveOverrides" :locale="zhCN" :date-locale="dateZhCN">
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

    <!-- 初始化引导弹窗：挂全局（自动弹出可能发生在任意路由），open 源在 store，
         首页入口（手动模式）与首访自动弹出共用一个实例 -->
    <SetupWizard :open="store.wizardOpen" @close="store.wizardOpen = false" />

    <!-- 内核下载进度弹窗（下载中不可关闭防误触丢进度视图，中断走「取消下载」按钮） -->
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
        <n-button size="small" quaternary type="error" :disabled="!dlBusy" @click="cancelCoreDownload">取消下载</n-button>
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
  padding: 8px 10px 40px;
}
@media (max-width: 760px) {
  .layout { flex-direction: column; }
  .content { padding: 8px 10px 30px; }
}
.dl-body { display: flex; flex-direction: column; align-items: center; gap: 14px; padding: 6px 0 2px; }
.dl-text { font-size: 13px; text-align: center; min-height: 18px; }
.dl-hint { color: var(--text-dim); font-size: 12px; }
</style>
