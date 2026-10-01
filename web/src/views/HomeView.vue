<script setup>
// 首页：运行状态、当前订阅、流量概览、快速切换节点
import { computed, onMounted, ref, watch } from 'vue'
import { NButton, NCard, NEmpty, NFlex, NInput, NModal, NProgress, NTag, NSwitch } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, fmtRate, fmtBytes, fmtUptime, tripTotals, resetTrip, delayColor } from '../store.js'
import Sparkline from '../components/Sparkline.vue'
import SubFormModal from '../components/SubFormModal.vue'
import AppIcon from '../components/AppIcon.vue'

const busy = ref('')

// hero 控制按钮同高同宽的方形文字钮（icon+文字），启停随状态换图标与文案
const HERO_H = 62
const heroBtn = { width: '94px', height: HERO_H + 'px', padding: '0', fontSize: '15px' }
const proxies = ref({})
const activeProfile = ref(null) // 当前激活订阅的完整信息（含流量）
const subBusy = ref(false)

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

// ---- 添加订阅弹窗（首页原地弹出，不跳转订阅页） ----
const showAdd = ref(false)

async function onSubAdded(p) {
  await loadProfiles()
  await refreshStatus()
  // 机场后加的订阅不会自动启用（首个订阅除外），提示去哪启用
  if (p?.id && activeProfile.value?.id && p.id !== activeProfile.value.id) {
    toast('新订阅未启用，可用首页「切换订阅」或订阅页「启用」', 'info', 5000)
  }
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

// ---- 快速切换：分组手风琴，默认全展开、可折叠；节点点击即切换 ----
const folded = ref({}) // 组名 → 是否折叠（缺省 false = 展开）

function delayOf(nodeName) {
  const h = proxies.value[nodeName]?.history
  return Array.isArray(h) && h.length ? h[h.length - 1].delay : 0
}

async function pick(g, node) {
  try {
    await api.put('/api/proxies/' + encodeURIComponent(g.name), { name: node })
    toast(`「${g.name}」已切换到 ${node}`, 'success')
    loadProxies()
  } catch (e) {
    toast(e.message, 'error')
  }
}

// ---- 流量里程表：显示 内核累计 - 基线，清零即从当前值重新统计 ----
const trip = computed(() => tripTotals())

function onTripReset() {
  resetTrip()
  toast('累计流量已清零，重新开始统计', 'success')
}

// ---- 出站模式 / DNS 快捷切换（瓦片齿轮 → 弹窗） ----
const MODE_LABEL = { rule: '规则', global: '全局', direct: '直连' }
const MODES = [
  { value: 'rule', label: '规则', desc: '按订阅规则分流：国内直连、代理流量按规则匹配（推荐）' },
  { value: 'global', label: '全局', desc: '所有连接都走代理节点，不按规则分流' },
  { value: 'direct', label: '直连', desc: '所有连接都直连，临时完全不经过代理' },
]
const showMode = ref(false)
const modeBusy = ref('')

async function applyMode(mode) {
  if (modeBusy.value || (status.value?.mode || 'rule') === mode) return
  modeBusy.value = mode
  try {
    const r = await api.put('/api/core/mode', { mode })
    if (store.status) store.status = { ...store.status, mode: r.mode }
    toast('出站模式已切换为「' + (MODE_LABEL[r.mode] || r.mode) + '」', 'success')
    showMode.value = false
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    modeBusy.value = ''
  }
}

const showDns = ref(false)
const dnsDraft = ref({ dns: true, dns_mode: 'fake-ip' })
const dnsBusy = ref(false)

function openDns() {
  dnsDraft.value = { dns: !!status.value?.dns, dns_mode: status.value?.dns_mode || 'fake-ip' }
  showDns.value = true
}

// DNS 走完整设置接口：先取全量再改两个字段回写（PUT 是整体替换）
async function applyDns() {
  if (dnsBusy.value) return
  dnsBusy.value = true
  try {
    const s = await api.get('/api/settings')
    s.dns = dnsDraft.value.dns
    s.dns_mode = dnsDraft.value.dns_mode
    const r = await api.put('/api/settings', s)
    toast('DNS 设置已保存' + (r.restarted ? '，内核已重启生效' : ''), 'success')
    showDns.value = false
    refreshStatus()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    dnsBusy.value = false
  }
}

// ---- 混合端口使用说明（瓦片齿轮 → 弹窗） ----
const showPort = ref(false)
// 弹窗示例里的主机地址：直接用打开本页面所用的地址（即路由器 IP）
const locationHost = location.hostname || '<路由器IP>'

const portHelp = computed(() => {
  const h = location.hostname || '<路由器IP>'
  const p = status.value?.mixed_port || 7890
  return [
    { os: 'Windows', steps: `设置 → 网络和 Internet → 代理 → 手动设置代理：地址 ${h}，端口 ${p}，保存即生效` },
    { os: 'macOS', steps: `系统设置 → 网络 → 详细信息 → 代理：开启「网页代理 (HTTP)」和「安全网页代理 (HTTPS)」，地址 ${h} 端口 ${p}` },
    { os: 'iOS / iPadOS', steps: `设置 → Wi-Fi → 当前网络 (i) → 配置代理 → 手动：服务器 ${h}，端口 ${p}` },
    { os: 'Android', steps: `设置 → WLAN → 长按当前网络 → 修改网络 → 高级 → 代理选「手动」：主机 ${h}，端口 ${p}` },
    { os: '浏览器插件', steps: `SwitchyOmega 等代理插件：HTTP 代理填 ${h}:${p}（SOCKS5 亦共用此端口）` },
    { os: '终端（macOS / Linux）', steps: `export https_proxy=http://${h}:${p} http_proxy=http://${h}:${p}` },
  ]
})

// status 晚于挂载到达时，运行起来后补一次代理列表
watch(
  () => store.status?.running,
  (running, prev) => {
    if (running && !prev) loadProxies()
  },
)

onMounted(() => {
  loadProxies()
  loadProfiles()
})

function currentOf(g) {
  const p = proxies.value[g.name]
  return p?.now || '—'
}
</script>

<template>
  <div class="page">
    <!-- 运行状态 -->
    <n-card>
      <n-flex vertical :size="16">
        <div class="run-badge" :class="{ on: status?.running, starting: !status?.running && status?.starting }">
          <span class="pulse"></span>
          <span class="run-text">{{ status?.running ? '运行中' : status?.starting ? '启动中…' : '已停止' }}</span>
        </div>
        <!-- 第二行放瓦片与按钮，align 居中让按钮与瓦片严格水平对齐 -->
        <n-flex align="center" :size="18">
          <n-flex class="hero-tiles" :size="16">
            <div class="meta-item">
              <span class="k"><AppIcon name="cpu" :size="13" />内核版本</span>
              <span class="v mono">{{ status?.core?.version || '未安装' }}</span>
            </div>
            <div class="meta-item">
              <span class="k"><AppIcon name="server" :size="13" />平台架构</span>
              <span class="v mono" :class="{ dim: !status?.core?.platform }">
                {{ status?.core?.platform ? status.core.platform.replace(/^linux-/, '') : '不支持' }}
              </span>
            </div>
            <div class="meta-item">
              <span class="k"><AppIcon name="clock" :size="13" />运行时长</span>
              <span class="v mono">{{ status?.running ? fmtUptime(status.uptime) : '—' }}</span>
            </div>
          </n-flex>
          <!-- margin-left:auto 而非 justify=space-between：窄屏下按钮组换行独占一行时仍贴右缘 -->
          <n-flex class="hero-actions" :size="12">
            <n-button
              title="重启内核"
              :style="heroBtn"
              :loading="busy === 'restart'" :disabled="!status?.running || busy !== ''" @click="coreAction('restart')"
            >
              <template #icon><AppIcon name="restart" :size="18" /></template>重启
            </n-button>
            <!-- 启动/停止同一个按钮：停止态主色「启动」，运行态红色幽灵「停止」 -->
            <n-button
              :type="status?.running ? 'error' : 'primary'"
              :ghost="!!status?.running"
              :title="status?.running ? '停止内核' : '启动内核'"
              :style="heroBtn"
              :loading="busy === 'start' || busy === 'stop'"
              :disabled="busy === 'restart'"
              @click="coreAction(status?.running ? 'stop' : 'start')"
            >
              <template #icon>
                <AppIcon v-if="!status?.running" name="play" :size="18" />
                <AppIcon v-else name="stop" :size="16" :stroke-width="2.4" />
              </template>{{ status?.running ? '停止' : '启动' }}
            </n-button>
          </n-flex>
        </n-flex>
      </n-flex>
    </n-card>

    <!-- 订阅行：当前订阅与右侧三张快捷卡同行；订阅卡右缘与上/下方瓦片组的第三列竖线对齐 -->
    <div class="sub-row-cards">
      <n-card class="sub-card">
        <div class="sec-head">
          <h3><AppIcon class="sec-ico" name="file-text" :size="15" />当前订阅</h3>
          <div class="sub-actions">
            <n-button size="small" title="刷新当前订阅" :loading="subBusy" :disabled="!activeProfile" @click="refreshProfile">
              <template #icon><AppIcon name="refresh" :size="13" /></template>刷新
            </n-button>
            <n-button v-if="status?.running" size="small" title="查看运行时配置（config.yaml）" @click="viewConfig">
              <template #icon><AppIcon name="file-code" :size="13" /></template>查看
            </n-button>
            <n-button size="small" title="切换订阅" @click="openSwitch">
              <template #icon><AppIcon name="swap" :size="13" /></template>切换
            </n-button>
            <n-button size="small" title="添加订阅" @click="showAdd = true">
              <template #icon><AppIcon name="plus" :size="13" /></template>添加
            </n-button>
          </div>
        </div>
        <n-empty v-if="!status?.profile" description="未设置订阅，请先选择并启用" />
        <template v-else>
          <div class="sub-row">
            <span class="sub-name">{{ status.profile }}</span>
            <span v-if="subExpire" class="page-sub">到期 {{ subExpire }}</span>
          </div>
          <div v-if="subTraffic" class="sub-traffic">
            <n-progress
              class="sub-bar"
              type="line"
              :percentage="subTraffic.percent"
              :show-indicator="false"
              :height="6"
              border-radius="3px"
            />
            <span class="mono page-sub">
              已用 {{ fmtBytes(subTraffic.used) }} / {{ fmtBytes(subTraffic.total) }}（{{ Math.round(subTraffic.percent) }}%）
            </span>
          </div>
          <div v-else class="page-sub" style="margin-top:4px">机场未提供流量信息</div>
        </template>
      </n-card>
      <n-card class="quick-card">
        <div class="quick-head">
          <span class="k"><AppIcon name="plug" :size="13" />混合端口</span>
          <button class="tile-gear" title="混合端口使用说明" @click="showPort = true">
            <AppIcon name="gear" :size="12" />
          </button>
        </div>
        <div class="quick-body mono">{{ status?.mixed_port || '—' }}</div>
      </n-card>
      <n-card class="quick-card">
        <div class="quick-head">
          <span class="k"><AppIcon name="globe" :size="13" />DNS</span>
          <button class="tile-gear" title="DNS 设置" @click="openDns">
            <AppIcon name="gear" :size="12" />
          </button>
        </div>
        <div class="quick-body mono" :class="{ dim: !status?.dns }">
          {{ status?.dns ? (status.dns_mode === 'fake-ip' ? 'Fake-IP' : status.dns_mode) : '未接管' }}
        </div>
      </n-card>
      <n-card class="quick-card">
        <div class="quick-head">
          <span class="k"><AppIcon name="layers" :size="13" />出站模式</span>
          <button class="tile-gear" title="切换出站模式" @click="showMode = true">
            <AppIcon name="gear" :size="12" />
          </button>
        </div>
        <div class="quick-body">
          <n-tag size="medium" round :bordered="false">{{ MODE_LABEL[status?.mode] || '规则' }}</n-tag>
        </div>
      </n-card>
    </div>

    <!-- 流量 -->
    <n-card>
      <div class="traffic-head">
        <h3><AppIcon class="sec-ico" name="activity" :size="15" />实时流量</h3>
        <!-- 指标做成小卡片瓦片：图标 + 标签在上、数值在下；连接/CPU/内存移至侧栏底部 -->
        <div class="traffic-nums">
          <div class="meta-item">
            <span class="k"><AppIcon name="upload" :size="13" />上传</span>
            <span class="v mono" style="color: var(--green)">{{ fmtRate(traffic.up) }}</span>
          </div>
          <div class="meta-item">
            <span class="k"><AppIcon name="download" :size="13" />下载</span>
            <span class="v mono" style="color: var(--accent)">{{ fmtRate(traffic.down) }}</span>
          </div>
          <div class="meta-item">
            <span class="k"><AppIcon name="history" :size="13" />累计上传</span>
            <span class="v mono" style="color: var(--green)">{{ fmtBytes(trip.up) }}</span>
          </div>
          <div class="meta-item">
            <span class="k"><AppIcon name="history" :size="13" />累计下载</span>
            <span class="v mono" style="color: var(--accent)">{{ fmtBytes(trip.down) }}</span>
          </div>
          <n-button
            class="trip-reset" quaternary :style="{ height: '69px' }"
            title="里程清零：丢弃当前累计，从零重新统计" @click="onTripReset"
          >
            <template #icon><AppIcon name="restart" :size="15" /></template>清零
          </n-button>
        </div>
      </div>
      <Sparkline v-if="status?.running" :series="store.history" :height="130" />
      <n-empty v-else description="内核未运行" style="padding: 40px 0" />
    </n-card>

    <!-- 切换节点：分组手风琴，默认全展开；容器限高，超出出现竖向滚动 -->
    <n-card>
      <div class="sec-head">
        <h3><AppIcon class="sec-ico" name="shuffle" :size="15" />切换节点</h3>
        <span class="page-sub">点击节点名直接切换 · 点击分组标题可折叠</span>
      </div>
      <n-empty v-if="!status?.running" description="内核未运行，启动后可切换节点" />
      <n-empty v-else-if="selectableGroups.length === 0" description="订阅中没有可手动选择的代理组" />
      <div v-else class="group-acc">
        <div v-for="g in selectableGroups" :key="g.name" class="grp">
          <button class="grp-head" @click="folded[g.name] = !folded[g.name]">
            <span class="g-name">{{ g.name }}</span>
            <span class="g-count">{{ g.all.length }} 节点</span>
            <span class="g-now">{{ currentOf(g) }}</span>
            <span class="g-arrow" :class="{ open: !folded[g.name] }">›</span>
          </button>
          <div v-show="!folded[g.name]" class="grp-body">
            <button
              v-for="node in g.all"
              :key="node"
              class="node-card"
              :class="{ on: node === currentOf(g) }"
              @click="pick(g, node)"
            >
              <span class="n-name">{{ node }}</span>
              <span class="n-delay mono" :style="{ color: delayColor(delayOf(node)) }">
                {{ delayOf(node) > 0 ? delayOf(node) + ' ms' : '' }}
              </span>
            </button>
          </div>
        </div>
      </div>
    </n-card>

    <!-- 添加订阅弹窗（首页原地弹出） -->
    <SubFormModal :open="showAdd" @close="showAdd = false" @added="onSubAdded" />

    <!-- 切换订阅弹窗 -->
    <n-modal
      preset="card"
      title="切换订阅"
      :show="showSwitch"
      :style="{ width: '520px', maxWidth: '94vw' }"
      @update:show="showSwitch = false"
    >
      <div class="switch-body">
        <n-input v-model:value="switchKeyword" placeholder="按名称搜索订阅，支持关键字模糊匹配…" clearable />
        <n-empty
          v-if="switchLoading"
          description="加载中…"
          style="padding: 40px 0"
        />
        <n-empty
          v-else-if="!switchFiltered.length"
          :description="switchList.length ? '没有匹配的订阅' : '还没有订阅，请先到「订阅」页添加'"
          style="padding: 40px 0"
        />
        <div v-else class="switch-list">
          <button
            v-for="p in switchFiltered"
            :key="p.id"
            class="switch-row"
            :class="{ sel: switchSelected === p.id }"
            @click="switchSelected = p.id"
          >
            <span class="s-name">{{ p.name }}</span>
            <n-tag v-if="switchActive === p.id" size="small" round :bordered="false">当前</n-tag>
          </button>
        </div>
        <div class="switch-foot">
          <span class="page-sub">选中后需确认才会切换并重载内核</span>
          <n-button
            type="primary"
            :loading="switching"
            :disabled="!switchSelected || switchSelected === switchActive"
            @click="confirmSwitch"
          >
            <template #icon><AppIcon name="check" :size="14" /></template>确认切换
          </n-button>
        </div>
      </div>
    </n-modal>

    <!-- 出站模式切换弹窗：点选项立即生效 -->
    <n-modal
      preset="card"
      title="出站模式"
      :show="showMode"
      :style="{ width: '440px', maxWidth: '94vw' }"
      @update:show="showMode = false"
    >
      <div class="mode-body">
        <button
          v-for="m in MODES"
          :key="m.value"
          class="mode-row"
          :class="{ sel: (status?.mode || 'rule') === m.value }"
          :disabled="!!modeBusy"
          @click="applyMode(m.value)"
        >
          <span class="m-name">
            {{ m.label }}
            <n-tag v-if="(status?.mode || 'rule') === m.value" size="small" round :bordered="false">当前</n-tag>
            <span v-if="modeBusy === m.value" class="m-busy">切换中…</span>
          </span>
          <span class="m-desc">{{ m.desc }}</span>
        </button>
        <div class="page-sub" style="margin-top:2px">运行中的内核立即生效，选择会记住，重启后仍生效</div>
      </div>
    </n-modal>

    <!-- DNS 设置弹窗：接管开关 + 解析模式 -->
    <n-modal
      preset="card"
      title="DNS 设置"
      :show="showDns"
      :style="{ width: '440px', maxWidth: '94vw' }"
      @update:show="showDns = false"
    >
      <div class="mode-body">
        <div class="dns-row">
          <div class="dns-text">
            <span class="m-name">DNS 接管</span>
            <span class="m-desc">由内核接管局域网 DNS 解析</span>
          </div>
          <n-switch v-model:value="dnsDraft.dns" size="small" />
        </div>
        <button
          class="mode-row"
          :class="{ sel: dnsDraft.dns_mode === 'fake-ip' }"
          :disabled="!dnsDraft.dns || dnsBusy"
          @click="dnsDraft.dns_mode = 'fake-ip'"
        >
          <span class="m-name">fake-ip</span>
          <span class="m-desc">返回假 IP，命中快、兼容性最好（推荐）</span>
        </button>
        <button
          class="mode-row"
          :class="{ sel: dnsDraft.dns_mode === 'redir-host' }"
          :disabled="!dnsDraft.dns || dnsBusy"
          @click="dnsDraft.dns_mode = 'redir-host'"
        >
          <span class="m-name">redir-host</span>
          <span class="m-desc">返回真实 IP，个别不支持假 IP 的设备更稳</span>
        </button>
        <div class="mode-foot">
          <span class="page-sub">保存后内核自动重启生效</span>
          <n-button type="primary" size="small" :loading="dnsBusy" @click="applyDns">保存</n-button>
        </div>
      </div>
    </n-modal>

    <!-- 混合端口使用说明 -->
    <n-modal
      preset="card"
      title="混合端口使用说明"
      :show="showPort"
      :style="{ width: '560px', maxWidth: '94vw' }"
      @update:show="showPort = false"
    >
      <div class="help-body">
        <div class="help-endpoint mono">{{ locationHost }}:{{ status?.mixed_port || 7890 }}</div>
        <div class="help-sub">HTTP 与 SOCKS5 共用此端口，任选其一；插件已允许局域网设备连接（allow-lan）。</div>
        <div v-for="s in portHelp" :key="s.os" class="help-sec">
          <div class="h-os">{{ s.os }}</div>
          <div class="h-steps">{{ s.steps }}</div>
        </div>
        <div class="help-note">
          代理连接本身无需任何令牌——「界面访问令牌」只保护本管理页面，与代理无关。
          开启 TUN 或透明代理模式时，局域网设备无需任何配置即自动接管，此端口供手动指定代理的设备使用。
        </div>
      </div>
    </n-modal>

    <!-- 运行时配置查看 -->
    <n-modal
      preset="card"
      title="运行时配置（config.yaml）"
      :show="showConfig"
      :style="{ width: '760px', maxWidth: '94vw' }"
      @update:show="showConfig = false"
    >
      <pre v-if="!cfgLoading" class="cfg-view mono">{{ cfgContent }}</pre>
      <n-empty v-else description="加载中…" style="padding: 40px 0" />
    </n-modal>
  </div>
</template>

<style scoped>
.run-badge { display: flex; align-items: center; gap: 10px; }
.run-text { font-size: 21px; font-weight: 700; }
.pulse { width: 12px; height: 12px; border-radius: 50%; background: #5a5f6d; position: relative; }
.pulse::after { content: ''; position: absolute; inset: -5px; border-radius: 50%; border: 2px solid transparent; }
.run-badge.on .pulse { background: var(--green); }
.run-badge.on .pulse::after { border-color: var(--green); animation: ring 1.8s ease-out infinite; }
.run-badge.starting .pulse { background: var(--orange); }
.run-badge.starting .pulse::after { border-color: var(--orange); animation: ring 1.4s ease-out infinite; }
@keyframes ring {
  0% { transform: scale(0.5); opacity: 1; }
  100% { transform: scale(1.5); opacity: 0; }
}
/* 状态项做成小卡片瓦片：浅底圆角，标签在上、值在下 */
.meta-item {
  display: flex; flex-direction: column; gap: 4px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 9px 14px 10px;
  min-width: 104px;
}
.meta-item .k { display: flex; align-items: center; gap: 6px; color: var(--text-dim); font-size: 11.5px; }
/* 瓦片标题行尾的小齿轮：默认很淡，悬停亮起 */
.tile-gear {
  margin-left: auto;
  display: inline-flex; align-items: center; justify-content: center;
  width: 18px; height: 18px; padding: 0;
  border: none; border-radius: 5px;
  background: transparent; cursor: pointer;
  color: currentColor; opacity: 0.55;
}
.tile-gear:hover { opacity: 1; color: var(--accent); background: var(--hover); }

/* 出站模式 / DNS 弹窗的选项卡片 */
.mode-body { display: flex; flex-direction: column; gap: 8px; }
.mode-row {
  display: flex; flex-direction: column; gap: 3px;
  text-align: left;
  padding: 10px 12px; border-radius: 10px;
  background: var(--bg-card-2);
  border: 1.5px solid var(--border);
  cursor: pointer; font: inherit; color: inherit;
}
.mode-row:disabled { opacity: 0.55; cursor: default; }
.mode-row.sel { border-color: var(--accent); background: var(--accent-soft); }
.mode-row .m-name { display: flex; align-items: center; gap: 8px; font-weight: 600; font-size: 13.5px; }
.mode-row .m-desc { color: var(--text-dim); font-size: 12px; }
.m-busy { color: var(--text-dim); font-size: 11.5px; font-weight: 400; }
.dns-row {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  padding: 10px 12px; border-radius: 10px;
  background: var(--bg-card-2);
  border: 1.5px solid var(--border);
}
.dns-row .dns-text { display: flex; flex-direction: column; gap: 3px; }
.mode-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 2px; }

/* 混合端口使用说明弹窗 */
.help-body { display: flex; flex-direction: column; gap: 10px; }
.help-endpoint {
  align-self: center;
  font-size: 20px; font-weight: 700;
  color: var(--accent); background: var(--accent-soft);
  border-radius: 10px; padding: 10px 18px;
}
.help-sub { color: var(--text-dim); font-size: 12.5px; text-align: center; }
.help-sec {
  display: flex; flex-direction: column; gap: 4px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px;
}
.h-os { font-weight: 600; font-size: 13px; }
.h-steps { color: var(--text-dim); font-size: 12.5px; line-height: 1.6; }
.help-note {
  color: var(--text-dim); font-size: 12px; line-height: 1.7;
  border-top: 1px dashed var(--border); padding-top: 10px;
}
.meta-item .v { font-size: 13.5px; font-weight: 600; }
.meta-item .v.dim { color: var(--text-dim); font-weight: 500; }

/* 标题独占一行，指标瓦片与清零按钮在标题下方独立成行；间隙与顶部卡片行一致（16px） */
.traffic-head { margin-bottom: 0; }
.traffic-nums { display: flex; gap: 16px; flex-wrap: wrap; align-items: stretch; margin: 12px 0 8px; }
/* 流量瓦片与运行卡/订阅行瓦片同一列宽基准：(50% − 24px) / 3；清零钮靠右 */
.traffic-nums .meta-item { height: 69px; flex: 0 1 calc((50% - 24px) / 3); }
.traffic-nums .meta-item .v { font-size: 16px; }
.trip-reset { flex: none; margin-left: auto; }

/* 运行卡右侧按钮组：靠右且窄屏换行后仍贴右缘 */
.hero-actions { margin-left: auto; }
/* 运行卡左瓦片组固定占半行：瓦片列宽与流量卡瓦片完全对齐
   （卡片内容宽 = 页宽 − 2×24 内边距，故 50% + 8px 恰好等于页宽的一半） */
.hero-tiles { flex: 0 1 calc(50% + 8px); }
.hero-tiles .meta-item { flex: 1 1 0; }

/* 订阅行：订阅卡与三张快捷卡同行。订阅卡右缘取 calc(50% + 8px)——恰好压在
   运行卡三块瓦片、流量卡前三块瓦片的右缘那条竖线上（卡内容宽 = 页宽 − 48），
   垂直看下来三处对齐；快捷卡平分剩余宽度 */
.sub-row-cards { display: flex; gap: 16px; align-items: stretch; }
.sub-card { flex: 0 0 calc(50% + 8px); min-width: 0; }
.quick-card { flex: 1 1 0; min-width: 0; }
/* 快捷卡：标题+齿轮置顶成头部行，下方内容区吃满剩余高度、大字号显示值 */
.quick-card :deep(.n-card-content) { display: flex; flex-direction: column; }
.quick-head { display: flex; align-items: center; gap: 6px; color: var(--text-dim); font-size: 11.5px; }
/* 行内 SVG 默认按基线对齐会偏低：icon 与标题用 flex 垂直居中 */
.quick-head .k { display: inline-flex; align-items: center; gap: 6px; }
.quick-head .tile-gear { margin-left: auto; }
.quick-body { flex: 1; display: flex; align-items: center; font-size: 20px; font-weight: 600; min-width: 0; }
.quick-body .dim, .quick-body.dim { color: var(--text-dim); font-weight: 500; }

.sec-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.sec-head h3 { font-size: 15px; }
/* 大卡片标题图标：主题色，行内基线微调对齐文字（sec-head / traffic-head 通用） */
.sec-ico { color: var(--accent); margin-right: 7px; vertical-align: -2px; }
.sub-actions { display: flex; gap: 8px; }
.sub-row { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.sub-name { font-size: 15px; font-weight: 600; }
.sub-traffic { display: flex; align-items: center; gap: 12px; margin-top: 9px; flex-wrap: wrap; }
.sub-bar { width: 260px; max-width: 50%; }
.group-acc {
  display: flex; flex-direction: column; gap: 10px;
  max-height: 60vh; overflow-y: auto;
}
.grp-head {
  display: flex; align-items: center; gap: 12px;
  width: 100%; text-align: left;
  padding: 12px 14px; border-radius: 11px;
  background: var(--bg-card-2);
  border: none; cursor: pointer;
  font: inherit; color: inherit;
}
.grp-head:hover { background: var(--hover); }
.g-name { font-weight: 600; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.g-count { color: var(--text-dim); font-size: 12px; flex: none; }
.g-now {
  color: var(--accent); font-size: 13px; max-width: 40%;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.g-arrow { color: var(--text-dim); font-size: 18px; flex: none; transition: transform 0.15s; }
.g-arrow.open { transform: rotate(90deg); }
/* 节点卡片网格：随宽度自适应列数，卡片即点击目标 */
.grp-body {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: 8px;
  padding: 8px 0 2px;
}
.node-card {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  text-align: left;
  padding: 9px 11px; border-radius: 10px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  cursor: pointer; font: inherit; color: inherit;
}
.node-card:hover { background: var(--hover); }
.node-card.on { border-color: var(--accent); background: var(--accent-soft); }
.n-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 500; }
.node-card.on .n-name { color: var(--accent); font-weight: 600; }
.n-delay { font-size: 12px; flex: none; }

.switch-body { display: flex; flex-direction: column; gap: 12px; }
.switch-list { display: flex; flex-direction: column; gap: 6px; max-height: 46vh; overflow-y: auto; }
.switch-row {
  display: flex; align-items: center; gap: 10px;
  width: 100%; text-align: left;
  padding: 11px 14px; border-radius: 11px;
  background: var(--bg-card-2);
  border: 1.5px solid transparent;
  cursor: pointer; font: inherit; color: inherit;
}
.switch-row:hover { background: var(--hover); }
.switch-row.sel { border-color: var(--accent); background: var(--accent-soft); }
.s-name {
  flex: 1; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-weight: 600; font-size: 13.5px;
}
.switch-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
}
.cfg-view {
  margin: 0; padding: 14px;
  overflow: auto; max-height: 70vh;
  background: var(--bg-card-2);
  border: 1px solid var(--border); border-radius: 10px;
  font-size: 12px; line-height: 1.6;
  white-space: pre; tab-size: 2;
}

/* ---- 手机/平板：瓦片与卡片改为可换行的窄列，避免挤压 ---- */
@media (max-width: 760px) {
  /* 运行卡瓦片占满一行三等分，控制按钮组换行靠右 */
  .hero-tiles { flex: 1 1 100%; }
  .hero-tiles .meta-item { min-width: 0; padding: 8px 10px 9px; }
  /* 订阅卡拉通整行（容器换行 + 0 0 100% 强制快捷卡换到下一行），快捷卡三等分一行并收紧内边距与字号 */
  .sub-row-cards { flex-wrap: wrap; }
  .sub-card { flex: 0 0 100%; }
  .quick-card :deep(.n-card-content) { padding: 10px 12px 12px; }
  .quick-head { font-size: 11px; gap: 5px; }
  .quick-head .k { gap: 5px; }
  .quick-head .tile-gear { width: 16px; height: 16px; }
  .quick-body { font-size: 16px; }
  /* 流量瓦片 2×2 */
  .traffic-nums { gap: 10px; }
  .traffic-nums .meta-item { flex: 1 1 calc(50% - 5px); height: 62px; }
  .traffic-nums .meta-item .v { font-size: 14px; }
  .sub-bar { max-width: 100%; }
}
</style>
