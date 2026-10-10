<script setup>
// 初始化引导弹窗（首页入口）：三步引导新用户完成「下载内核 → 添加订阅 → 启动运行」。
// 每次打开都按设备实际状态自动定位到第一个未完成的步骤（已完成的步骤直接显示结果），
// 因此任何时候打开都安全，老用户误入也会落在需要补的步骤或直接看到「一切就绪」。
//   步骤 1 —— 选择内核渠道（Release/Alpha）并从 GitHub 下载安装（长任务 + 进度轮询，
//             页面刷新或弹窗关闭后重新打开可恢复跟进进行中的任务）；
//   步骤 2 —— 内嵌订阅表单（复用 SubFormBody）：订阅链接或手动节点两种来源，
//             添加成功后确保该订阅处于激活态（后端仅首个订阅自动激活，此处兜底）；
//   步骤 3 —— 汇总校验内核与订阅是否到位，一键启动内核并轮询直到运行中。
// 弹窗打开期间 store.wizardOpen 置真，App.vue 的「未检测到内核」自动询问会避让，
// 避免两处同时引导下载。
//
// 使用示例：
//   <SetupWizard :open="showWizard" @close="showWizard = false" />
import { computed, reactive, ref, watch } from 'vue'
import { NButton, NModal, NProgress, NStep, NSteps } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, fmtBytes } from '../store.js'
import AppIcon from './AppIcon.vue'
import SubFormBody from './SubFormBody.vue'

const props = defineProps({
  open: Boolean,
})
const emit = defineEmits(['close'])

const step = ref(1) // 当前步骤：1=下载内核 2=添加订阅 3=启动运行
const finished = ref(false) // 内核已运行（全部就绪）：步骤条全部点亮完成
const detecting = ref(false) // 打开时的状态检测进行中

// 步骤条 current：全就绪时推到 4，三步都显示完成态
const stepsCurrent = computed(() => (finished.value ? 4 : step.value))

const status = computed(() => store.status)

// 只读映射，避免模板里长表达式
const coreInstalled = computed(() => !!status.value?.core?.installed)
const hasProfile = computed(() => !!status.value?.profile)

// ---- 打开/关闭：同步全局向导标志（App.vue 守卫用），打开时定位步骤 ----
watch(
  () => props.open,
  (v) => {
    store.wizardOpen = v
    if (v) detect()
    else cleanup()
  },
)

// 关闭弹窗的统一出口：清理轮询与进行中状态
function close() {
  cleanup()
  emit('close')
}

function cleanup() {
  stopDlPoll()
  detectGen.value++
}

// ---- 状态检测与步骤定位 ----

// 检测代数：弹窗重开/关闭时自增，旧的异步流程（启动轮询等）发现代数变化即自行退出
const detectGen = ref(0)

// 打开引导时定位步骤：优先恢复进行中的内核下载任务，再按完成度推导
async function detect() {
  detecting.value = true
  finished.value = false
  try {
    const p = await api.get('/api/upgrade/progress')
    if (p?.active && p.kind === 'core') {
      step.value = 1
      dl.state = 'downloading'
      dl.prog = p
      startDlPoll(true)
      return
    }
  } catch { /* 查不到任务就按状态推导 */ }
  const s = await locateStep()
  if (s >= 3) {
    step.value = 3
    finished.value = s > 3
  } else {
    step.value = s
  }
}

// 按设备实际状态推导应停留的步骤：1 未装内核 / 2 无激活订阅 / 3 未运行 / 4 全部就绪
async function locateStep() {
  try { store.status = await api.get('/api/status') } catch { /* 用现有状态兜底 */ }
  const s = store.status
  if (!s?.core?.installed) return 1
  if (!s?.profile) return 2
  if (!s?.running) return 3
  return 4
}

// ---- 步骤 1：下载内核 ----

const CHANNELS = [
  { value: 'release', label: '正式版（Release）', desc: 'mihomo 官方稳定版本，日常使用推荐' },
  { value: 'alpha', label: '抢先版（Alpha）', desc: '跟随开发分支构建，新特性先到，可能不稳定' },
]
const channel = ref('release')

const dl = reactive({ state: '', prog: null, cancelled: false, error: '' })
let dlTimer = null

// 进度展示文本：阶段描述 + 已下载/总量（百分比）
const dlProgText = computed(() => {
  const p = dl.prog
  if (!p) return '正在连接…'
  const parts = [p.message]
  if (p.total > 0) parts.push(`${fmtBytes(p.downloaded)} / ${fmtBytes(p.total)}（${Math.round(p.percent)}%）`)
  else if (p.downloaded > 0) parts.push(fmtBytes(p.downloaded))
  return parts.filter(Boolean).join('，')
})

// 开始下载：先把所选渠道落盘（下载接口跟随后端渠道设置），再发起长任务并轮询进度。
// 渠道无变化时不动设置，避免无谓的整体替换。
async function startCoreDownload() {
  if (dl.state === 'downloading') return
  dl.cancelled = false
  dl.error = ''
  try {
    const s = await api.get('/api/settings')
    if ((s.core_channel || 'release') !== channel.value) {
      s.core_channel = channel.value
      await api.put('/api/settings', s)
    }
  } catch { /* 渠道落盘失败不拦下载：按后端当前渠道下载 */ }
  dl.state = 'downloading'
  dl.prog = null
  startDlPoll()
  try {
    await api.post('/api/core/upgrade')
    stopDlPoll()
    dl.state = ''
    dl.prog = null
    await advanceAfterCore()
  } catch (e) {
    stopDlPoll()
    if (dl.cancelled) {
      dl.state = '' // 用户主动取消：回到待下载态
      toast('已取消下载', 'info')
    } else {
      dl.state = 'error'
      dl.error = e.message
    }
  }
}

// 轮询下载进度（resume = 跟进页面刷新/重开弹窗前已在跑的任务，终态由轮询收尾；
// 正常发起的下载由升级接口自身返回收尾，防双推进）
function startDlPoll(resume = false) {
  stopDlPoll()
  const tick = async () => {
    try {
      dl.prog = await api.get('/api/upgrade/progress')
    } catch { return /* 单次失败下轮再试 */ }
    if (!(resume && dl.prog && !dl.prog.active)) return
    // 恢复模式：任务到终态在这里收尾
    stopDlPoll()
    if (dl.prog.stage === 'done') {
      dl.state = ''
      dl.prog = null
      await advanceAfterCore()
    } else {
      dl.state = 'error'
      dl.error = dl.prog.message || '下载失败'
    }
  }
  tick()
  dlTimer = setInterval(tick, 600)
}

function stopDlPoll() {
  if (dlTimer) { clearInterval(dlTimer); dlTimer = null }
}

// 取消进行中的下载：中断后端任务（升级接口会以「已取消」失败，由 catch 收尾）
async function cancelDownload() {
  dl.cancelled = true
  try { await api.post('/api/upgrade/cancel') } catch { /* 任务可能刚自行结束 */ }
}

// 内核就位后定位下一步：无订阅进步骤 2，已有订阅直接进步骤 3
async function advanceAfterCore() {
  const s = await locateStep()
  if (!s || s < 2) return // 状态未刷新到位时停在原步骤，下次打开再定位
  step.value = Math.min(s, 3)
  finished.value = s > 3
}

// ---- 步骤 2：添加订阅 ----

// 引导内添加订阅成功：后端只对首个订阅自动激活，这里兜底确保激活后进入下一步
async function onSubAdded(p) {
  try {
    await refreshStatusOnce()
    if (!store.status?.profile && p?.id) {
      await api.post(`/api/profiles/${p.id}/activate`)
      await refreshStatusOnce()
    }
  } catch (e) {
    toast(e.message, 'error', 6000)
    return
  }
  if (store.status?.profile) {
    toast('订阅已就绪', 'success')
    step.value = 3
  }
}

async function refreshStatusOnce() {
  store.status = await api.get('/api/status')
}

// ---- 步骤 3：启动内核 ----

const launch = reactive({ state: '', error: '' }) // '' 待启动 / starting / ok / error

// 启动内核并轮询直到运行中：start 接口返回即进程已拉起，之后等就绪探测结束
// （starting 归零且 running 仍为假 = 启动失败，如订阅内容无效）
async function launchCore() {
  if (launch.state === 'starting') return
  launch.state = 'starting'
  launch.error = ''
  const gen = detectGen.value
  try {
    await api.post('/api/core/start')
    for (let i = 0; i < 45; i++) {
      await new Promise(r => setTimeout(r, 1000))
      if (gen !== detectGen.value) return // 弹窗已关闭/重开，本轮作废
      try { await refreshStatusOnce() } catch { continue }
      if (store.status?.running) {
        launch.state = 'ok'
        finished.value = true
        toast('内核已启动，初始化完成', 'success', 5000)
        return
      }
      if (!store.status?.starting) break // 进程退出 = 启动失败
    }
    if (launch.state !== 'ok') {
      launch.state = 'error'
      launch.error = '内核未能进入运行状态，请到「日志」页查看原因'
    }
  } catch (e) {
    launch.state = 'error'
    launch.error = e.message
    toast(e.message, 'error', 6000)
  }
}
</script>

<template>
  <n-modal
    preset="card"
    title="初始化引导"
    :show="open"
    :style="{ width: '620px', maxWidth: '94vw' }"
    @update:show="close"
  >
    <div class="wiz">
      <n-steps :current="stepsCurrent" size="small" class="wiz-steps">
        <n-step title="下载内核" />
        <n-step title="添加订阅" />
        <n-step title="启动运行" />
      </n-steps>

      <div class="wiz-body">
        <!-- 步骤 1：下载内核 -->
        <div v-if="step === 1" class="wiz-pane">
          <template v-if="coreInstalled">
            <div class="done-row">
              <span class="done-ico"><AppIcon name="check" :size="16" /></span>
              <span class="done-text">
                内核已安装
                <span class="done-sub mono">
                  {{ status?.core?.version || '' }}{{ status?.core?.platform ? ' · ' + status.core.platform.replace(/^linux-/, '') : '' }}
                </span>
              </span>
            </div>
            <div class="wiz-foot">
              <span class="page-sub">内核已就位，无需重新下载</span>
              <n-button type="primary" @click="step = 2">
                下一步<template #icon><AppIcon name="chevron-right" :size="14" /></template>
              </n-button>
            </div>
          </template>
          <template v-else>
            <p class="wiz-desc">第一步先把 mihomo 内核下载到路由器。选择发布渠道，点击「开始下载」从 GitHub 拉取并自动安装：</p>
            <button
              v-for="c in CHANNELS"
              :key="c.value"
              class="wiz-opt"
              :class="{ sel: channel === c.value }"
              :disabled="dl.state === 'downloading'"
              @click="channel = c.value"
            >
              <span class="o-name">{{ c.label }}</span>
              <span class="o-desc">{{ c.desc }}</span>
            </button>
            <div v-if="dl.state === 'downloading'" class="dl-box">
              <div class="dl-text mono">{{ dlProgText }}</div>
              <n-progress
                v-if="dl.prog?.percent > 0"
                type="line"
                :percentage="dl.prog.percent"
                :show-indicator="false"
                :height="8"
                border-radius="4px"
              />
              <div class="dl-hint">视网络情况可能需要几分钟，可随时取消</div>
              <n-button size="small" quaternary type="error" @click="cancelDownload">取消下载</n-button>
            </div>
            <template v-else>
              <div v-if="dl.state === 'error'" class="err-row">
                <AppIcon name="x-square" :size="14" />
                <span>{{ dl.error || '下载失败' }}</span>
              </div>
              <div class="wiz-foot">
                <span class="page-sub">已安装的渠道内核与另一渠道互不覆盖，可稍后在设置中切换</span>
                <n-button type="primary" :loading="dl.state === 'downloading'" @click="startCoreDownload">
                  <template #icon><AppIcon name="download" :size="14" /></template>
                  {{ dl.state === 'error' ? '重试下载' : '开始下载' }}
                </n-button>
              </div>
            </template>
          </template>
        </div>

        <!-- 步骤 2：添加订阅 -->
        <div v-else-if="step === 2" class="wiz-pane">
          <template v-if="hasProfile">
            <div class="done-row">
              <span class="done-ico"><AppIcon name="check" :size="16" /></span>
              <span class="done-text">
                订阅已就绪
                <span class="done-sub">正在使用「{{ status?.profile }}」</span>
              </span>
            </div>
            <div class="wiz-foot">
              <span class="page-sub">已有激活的订阅，无需重复添加</span>
              <n-button type="primary" @click="step = 3">
                下一步<template #icon><AppIcon name="chevron-right" :size="14" /></template>
              </n-button>
            </div>
          </template>
          <template v-else>
            <p class="wiz-desc">第二步导入你的订阅：有机场订阅链接就选「订阅链接」；只有零散节点分享链接则选「手动节点」。添加后自动启用：</p>
            <SubFormBody embedded :show-cancel="false" :active="open && step === 2" @added="onSubAdded" />
          </template>
        </div>

        <!-- 步骤 3：启动运行 -->
        <div v-else class="wiz-pane">
          <div class="chk-list">
            <div class="chk-row">
              <span class="chk-ico ok"><AppIcon name="check" :size="13" /></span>
              <span>内核已安装 <span class="mono chk-sub">{{ status?.core?.version || '' }}</span></span>
            </div>
            <div class="chk-row">
              <span class="chk-ico" :class="hasProfile ? 'ok' : 'no'">
                <AppIcon :name="hasProfile ? 'check' : 'close'" :size="13" />
              </span>
              <span>
                {{ hasProfile ? `订阅已激活「${status?.profile}」` : '订阅未配置' }}
              </span>
            </div>
          </div>
          <template v-if="finished && launch.state === 'ok'">
            <div class="ok-panel">
              <span class="ok-ico"><AppIcon name="check" :size="20" /></span>
              <span class="ok-text">一切就绪，内核运行中</span>
              <span class="page-sub">局域网设备已可按混合端口 {{ status?.mixed_port || 7890 }} 使用代理；开启 TUN 或透明代理时自动接管，无需配置</span>
            </div>
            <div class="wiz-foot">
              <span class="page-sub">后续可在首页与设置页调整节点、模式等</span>
              <n-button type="primary" @click="close">
                <template #icon><AppIcon name="check" :size="14" /></template>完成
              </n-button>
            </div>
          </template>
          <template v-else>
            <p class="wiz-desc">最后一步启动内核。启动成功后本引导即完成：</p>
            <div v-if="launch.state === 'error'" class="err-row">
              <AppIcon name="x-square" :size="14" />
              <span>{{ launch.error || '启动失败' }}</span>
            </div>
            <div class="wiz-foot">
              <span class="page-sub">启动失败常见于订阅内容有误，可返回上一步重新添加</span>
              <n-button type="primary" :loading="launch.state === 'starting'" @click="launchCore">
                <template #icon><AppIcon name="play" :size="14" /></template>
                {{ launch.state === 'error' ? '重试启动' : '启动内核' }}
              </n-button>
            </div>
          </template>
        </div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.wiz { display: flex; flex-direction: column; gap: 16px; }
.wiz-steps { padding: 2px 6px 0; }
/* 内容区定高防步骤切换跳动，超出出竖向滚动 */
.wiz-body { min-height: 300px; max-height: 56vh; overflow-y: auto; }
.wiz-pane { display: flex; flex-direction: column; gap: 10px; }
.wiz-desc { margin: 0; color: var(--text-dim); font-size: 12.5px; line-height: 1.7; }
.wiz-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 4px; }
.wiz-foot .page-sub { text-align: right; }

/* 渠道选项卡：与首页出站模式弹窗同风格的可点击卡片 */
.wiz-opt {
  display: flex; flex-direction: column; gap: 3px;
  text-align: left;
  padding: 10px 12px; border-radius: 10px;
  background: var(--bg-card-2);
  border: 1.5px solid var(--border);
  cursor: pointer; font: inherit; color: inherit;
}
.wiz-opt:disabled { opacity: 0.55; cursor: default; }
.wiz-opt.sel { border-color: var(--accent); background: var(--accent-soft); }
.wiz-opt .o-name { font-weight: 600; font-size: 13.5px; }
.wiz-opt .o-desc { color: var(--text-dim); font-size: 12px; }

/* 下载进行中：浅底容器聚拢进度信息 */
.dl-box {
  display: flex; flex-direction: column; align-items: center; gap: 10px;
  padding: 16px 12px;
  background: var(--bg-card-2);
  border: 1px solid var(--border); border-radius: 10px;
}
.dl-text { font-size: 13px; text-align: center; min-height: 18px; }
.dl-box .n-progress { width: 100%; }
.dl-hint { color: var(--text-dim); font-size: 12px; }

/* 步骤完成态行：绿色对勾 + 结果描述 */
.done-row { display: flex; align-items: center; gap: 10px; padding: 14px 12px; }
.done-ico {
  display: inline-flex; align-items: center; justify-content: center;
  width: 30px; height: 30px; border-radius: 50%;
  color: var(--green); background: color-mix(in srgb, var(--green) 14%, transparent);
  flex: none;
}
.done-text { display: flex; flex-direction: column; gap: 2px; font-weight: 600; font-size: 14px; }
.done-sub { font-weight: 400; font-size: 12.5px; color: var(--text-dim); }

/* 步骤 3 校验清单 */
.chk-list {
  display: flex; flex-direction: column; gap: 8px;
  padding: 12px; border-radius: 10px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
}
.chk-row { display: flex; align-items: center; gap: 9px; font-size: 13.5px; }
.chk-ico {
  display: inline-flex; align-items: center; justify-content: center;
  width: 20px; height: 20px; border-radius: 50%; flex: none;
}
.chk-ico.ok { color: var(--green); background: color-mix(in srgb, var(--green) 14%, transparent); }
.chk-ico.no { color: var(--red, #e05555); background: color-mix(in srgb, #e05555 12%, transparent); }
.chk-sub { font-size: 12px; color: var(--text-dim); }

/* 全部完成面板 */
.ok-panel {
  display: flex; flex-direction: column; align-items: center; gap: 8px;
  padding: 22px 12px; text-align: center;
}
.ok-ico {
  display: inline-flex; align-items: center; justify-content: center;
  width: 44px; height: 44px; border-radius: 50%;
  color: var(--green); background: color-mix(in srgb, var(--green) 14%, transparent);
}
.ok-text { font-size: 16px; font-weight: 700; }

/* 错误行 */
.err-row {
  display: flex; align-items: center; gap: 8px;
  color: var(--red, #e05555); font-size: 12.5px;
  padding: 9px 12px;
  background: color-mix(in srgb, #e05555 8%, transparent);
  border-radius: 9px;
}

/* 手机端：说明文字与按钮组换行后按钮组占满一行靠右 */
@media (max-width: 760px) {
  .wiz-foot { flex-wrap: wrap; }
  .wiz-foot .n-button { margin-left: auto; }
}
</style>
