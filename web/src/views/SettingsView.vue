<script setup>
// 设置页：基础设置、TUN/DNS、内核更新（mihomo）、插件更新
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { NButton, NCard, NEmpty, NInput, NInputGroup, NInputNumber, NModal, NProgress, NSelect, NSpin, NSwitch, NTabs, NTabPane } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask, fmtTime } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

function fmtMB(n) {
  return (n / 1048576).toFixed(1) + ' MB'
}

// 备份列表用的小体积格式化（B/KB/MB 自适应）
function fmtSize(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

const form = reactive({
  mixed_port: 7890,
  ui_port: 9097,
  tun: false,
  tun_stack: 'mixed',
  dns: true,
  dns_mode: 'fake-ip',
  dns_hijack: 'firewall',
  dns_hijack_ipv4: true,
  dns_hijack_ipv6: false,
  token: '',
  core_arch: '',
  core_mem_limit: 0,
  download_proxy: 'https://gh-proxy.com',
})
const loaded = ref(false)
const saving = ref(false)

const coreInfo = ref({ installed: false, version: '', path: '', platform: '' })
const coreLatest = ref(null) // {current, latest, has_update}
const coreUpgrading = ref(false)
const pluginLatest = ref(null)
const pluginUpgrading = ref(false)

// 升级进度：升级期间轮询 /api/upgrade/progress
const prog = ref(null)
let progTimer = null
const progText = computed(() => {
  const p = prog.value
  if (!p) return ''
  const parts = [p.message]
  if (p.total > 0) parts.push(`${fmtMB(p.downloaded)} / ${fmtMB(p.total)}（${Math.round(p.percent)}%）`)
  else if (p.downloaded > 0) parts.push(fmtMB(p.downloaded))
  return parts.filter(Boolean).join('，')
})
const coreBtnText = computed(() =>
  coreUpgrading.value ? (prog.value?.percent > 0 ? `下载中 ${Math.round(prog.value.percent)}%` : '下载安装中…') : '升级')
const pluginBtnText = computed(() =>
  pluginUpgrading.value ? (prog.value?.percent > 0 ? `下载中 ${Math.round(prog.value.percent)}%` : '下载安装中…') : '升级')

// 随机生成 32 位十六进制令牌（crypto 安全随机）
function genToken() {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  form.token = [...b].map(x => x.toString(16).padStart(2, '0')).join('')
}

function startProgPoll() {
  stopProgPoll()
  prog.value = null
  const tick = async () => {
    try { prog.value = await api.get('/api/upgrade/progress') } catch { /* 轮询失败下次再试 */ }
  }
  tick()
  progTimer = setInterval(tick, 600)
}
function stopProgPoll() {
  if (progTimer) { clearInterval(progTimer); progTimer = null }
}

async function load() {
  try {
    const s = await api.get('/api/settings')
    Object.assign(form, {
      mixed_port: s.mixed_port,
      ui_port: s.ui_port,
      tun: s.tun,
      tun_stack: s.tun_stack,
      dns: s.dns,
      dns_mode: s.dns_mode || 'fake-ip',
      dns_hijack: s.dns_hijack || 'firewall',
      dns_hijack_ipv4: s.dns_hijack_ipv4 !== false,
      dns_hijack_ipv6: !!s.dns_hijack_ipv6,
      token: s.token || '',
      core_arch: s.core_arch || '',
      core_mem_limit: s.core_mem_limit || 0,
      download_proxy: s.download_proxy || '',
    })
    loaded.value = true
    coreInfo.value = await api.get('/api/core/status')
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function save() {
  saving.value = true
  try {
    // 合并回完整设置（保留后端管理的字段）；数字输入清空时兜底默认值
    const cur = await api.get('/api/settings')
    const payload = {
      ...cur,
      ...form,
      mixed_port: Number(form.mixed_port) || 7890,
      ui_port: Number(form.ui_port) || 9097,
      core_mem_limit: Math.max(0, Number(form.core_mem_limit) || 0),
    }
    const r = await api.put('/api/settings', payload)
    Object.assign(form, {
      mixed_port: r.settings.mixed_port,
      ui_port: r.settings.ui_port,
      tun: r.settings.tun,
      tun_stack: r.settings.tun_stack,
      dns: r.settings.dns,
      dns_mode: r.settings.dns_mode || 'fake-ip',
      dns_hijack: r.settings.dns_hijack || 'firewall',
      dns_hijack_ipv4: r.settings.dns_hijack_ipv4 !== false,
      dns_hijack_ipv6: !!r.settings.dns_hijack_ipv6,
      token: r.settings.token || '',
      core_arch: r.settings.core_arch || '',
      core_mem_limit: r.settings.core_mem_limit || 0,
      download_proxy: r.settings.download_proxy || '',
    })
    if (r.error) toast('已保存，但内核重启失败：' + r.error, 'error')
    else if (r.restarted) toast('已保存，内核已重载生效', 'success')
    else toast('已保存' + (r.need_reload ? '（令牌需重启服务后生效）' : ''), 'success')
    store.status = await api.get('/api/status')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    saving.value = false
  }
}

async function checkCore() {
  try {
    coreLatest.value = await api.get('/api/core/latest')
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function upgradeCore() {
  if (!(await ask('升级内核', `确认下载并安装 mihomo ${coreLatest.value.latest}？视网络情况可能需要几分钟`))) return
  coreUpgrading.value = true
  startProgPoll()
  try {
    const r = await api.post('/api/core/upgrade')
    toast(`内核已更新到 ${r.version}`, 'success', 5000)
    coreInfo.value = await api.get('/api/core/status')
    coreLatest.value = null
    store.status = await api.get('/api/status')
  } catch (e) {
    toast(e.message, 'error', 6000)
  } finally {
    stopProgPoll()
    coreUpgrading.value = false
  }
}

async function checkPlugin() {
  try {
    pluginLatest.value = await api.get('/api/plugin/latest')
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function upgradePlugin() {
  if (!(await ask('更新插件', '确认下载并安装插件最新版本？安装后需要重启服务'))) return
  pluginUpgrading.value = true
  startProgPoll()
  try {
    const r = await api.post('/api/plugin/upgrade')
    if (r.need_restart && store.status?.openwrt) {
      if (await ask('重启服务', '插件已下载，立即重启服务生效？')) {
        await api.post('/api/service/restart')
        toast('服务重启中，请稍后刷新页面', 'success')
      }
    } else {
      toast('插件已更新，请手动重启服务生效', 'success')
    }
  } catch (e) {
    toast(e.message, 'error', 6000)
  } finally {
    stopProgPoll()
    pluginUpgrading.value = false
  }
}

async function restartService() {
  if (!(await ask('重启服务', '确认重启 ClashV 服务？'))) return
  try {
    await api.post('/api/service/restart')
    toast('服务重启中，请稍后刷新页面', 'success')
  } catch (e) {
    toast(e.message, 'error')
  }
}

// 恢复出厂：除订阅配置（含激活项）外全部回到安装初始状态，二次确认防误触
const resetting = ref(false)

async function resetAll() {
  if (!(await ask('重置所有配置', '将停止内核，并把设置、自定义规则、运行状态、日志全部恢复到插件安装时的初始状态；订阅配置会保留。此操作不可恢复，确定继续？'))) return
  if (!(await ask('最终确认', '再次确认：立即重置所有配置？'))) return
  resetting.value = true
  try {
    await api.post('/api/plugin/reset')
    toast('已恢复出厂设置（订阅配置保留），内核已停止', 'success', 5000)
    await load()
    store.status = await api.get('/api/status')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    resetting.value = false
  }
}

// ---- 备份与恢复：全部设置+订阅+自定义规则打包为 zip，支持多份 ----
const backups = ref([])
const backupsLoading = ref(false)
const showBackupList = ref(false)
const restoringName = ref('')
const deletingName = ref('')
const showBackupCreate = ref(false)
const backupName = ref('')
const backupCreating = ref(false)

// 自动备份名：时间格式 + 随机串，用户可在弹窗里改名
function genBackupName() {
  const d = new Date()
  const p = n => String(n).padStart(2, '0')
  const rand = Array.from(crypto.getRandomValues(new Uint8Array(3)))
    .map(b => b.toString(16).padStart(2, '0')).join('')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}_${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}_${rand}`
}

async function refreshBackups() {
  backupsLoading.value = true
  try {
    const r = await api.get('/api/backup/list')
    backups.value = r.backups || []
  } catch { /* 静默，列表打开时会重试 */ } finally {
    backupsLoading.value = false
  }
}

function openBackupCreate() {
  backupName.value = genBackupName()
  showBackupCreate.value = true
}

async function createBackup() {
  if (!backupName.value.trim()) {
    toast('请填写备份名称', 'error')
    return
  }
  backupCreating.value = true
  try {
    await api.post('/api/backup/create', { name: backupName.value })
    toast('备份已创建', 'success')
    showBackupCreate.value = false
    refreshBackups()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    backupCreating.value = false
  }
}

function openBackupList() {
  showBackupList.value = true
  refreshBackups()
}

async function restoreBackup(b) {
  const running = store.status?.running
  if (!(await ask('恢复备份', `将用「${b.name}」覆盖当前全部设置、订阅配置与自定义规则${running ? '，内核会自动重启' : ''}。确定恢复？`))) return
  restoringName.value = b.name
  try {
    const r = await api.post('/api/backup/restore', { name: b.name })
    toast(r.restarted ? '备份已恢复，内核已重启生效' : '备份已恢复；内核当前未运行，可到首页启动', 'success', 5000)
    showBackupList.value = false
    await load()
    store.status = await api.get('/api/status')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    restoringName.value = ''
  }
}

async function deleteBackup(b) {
  if (!(await ask('删除备份', `确认删除备份「${b.name}」？删除后不可恢复`))) return
  deletingName.value = b.name
  try {
    await api.del('/api/backup/' + encodeURIComponent(b.name))
    toast('备份已删除', 'success')
    refreshBackups()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    deletingName.value = ''
  }
}

onMounted(() => { load(); refreshBackups() })
onBeforeUnmount(stopProgPoll)

// 设置分区 tab：通用（代理基础+访问控制）/ 网络 / 内核 / 插件
const tab = ref('general')
</script>

<template>
  <div class="page">
    <n-card class="head-card">
      <n-tabs v-model:value="tab" type="segment" size="small" class="tabs">
        <n-tab-pane name="general"><template #tab>通用</template></n-tab-pane>
        <n-tab-pane name="network"><template #tab>网络</template></n-tab-pane>
        <n-tab-pane name="core"><template #tab>内核</template></n-tab-pane>
        <n-tab-pane name="plugin"><template #tab>插件</template></n-tab-pane>
      </n-tabs>
    </n-card>

    <!-- 通用 -->
    <template v-if="tab === 'general'">
      <n-card title="代理基础">
        <div class="rows">
          <div class="row">
            <div class="row-text">
              <span class="rt">混合代理端口</span>
              <span class="rs">HTTP 与 SOCKS5 共用的代理端口，局域网设备手动配置代理时填它<span class="rec">默认 7890，建议保持，端口被占用再改</span></span>
            </div>
            <n-input-number v-model:value="form.mixed_port" :min="1" :max="65535" class="num" />
          </div>
          <div class="row">
            <div class="row-text">
              <span class="rt">界面访问令牌</span>
              <span class="rs">保护管理界面：设置后局域网内打开本页面需先输入令牌<span class="rec">默认不启用；局域网内使用建议设置，仅本机访问可留空</span></span>
            </div>
            <n-input-group class="ctl">
              <n-input
                v-model:value="form.token"
                type="password"
                show-password-on="click"
                placeholder="留空不启用"
                style="width: 220px"
              />
              <n-button title="随机生成 32 位令牌" @click="genToken">
                <template #icon><AppIcon name="zap" :size="14" /></template>
              </n-button>
              <n-button title="清空令牌" :disabled="!form.token" @click="form.token = ''">
                <template #icon><AppIcon name="trash" :size="14" /></template>
              </n-button>
            </n-input-group>
          </div>
        </div>
      </n-card>
    </template>

    <!-- 网络：TUN 与 DNS 拆成两张卡，互不混淆 -->
    <n-card v-if="tab === 'network'" title="TUN">
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">TUN 模式</span>
            <span class="rs">接管全局流量（含 UDP，需内核 tun 模块）；关闭时自动用防火墙接管局域网 TCP（透明代理），无需手动配置<span class="rec">默认关闭，建议保持；需接管 UDP/全局流量时再开</span></span>
          </div>
          <n-switch v-model:value="form.tun" />
        </div>
        <div class="row" v-if="form.tun">
          <div class="row-text">
            <span class="rt">TUN 协议栈</span>
            <span class="rs">system 走系统网络栈、gvisor 纯用户态实现，mixed 混合两者<span class="rec">默认 mixed，建议保持</span></span>
          </div>
          <n-select v-model:value="form.tun_stack" :options="[{ value: 'mixed', label: 'mixed' }, { value: 'system', label: 'system' }, { value: 'gvisor', label: 'gvisor' }]" class="ctl" style="width: 130px" />
        </div>
      </div>
    </n-card>

    <n-card v-if="tab === 'network'">
      <!-- 副标题挂在标题里：提示用户保持默认，弱化显示不抢选项行 -->
      <template #header>
        <div class="card-head">
          <span>DNS</span>
          <span class="card-sub">以下选项已按最常见的组网场景预设，保持默认即可稳定工作；确有需要再调整，保存后自动生效</span>
        </div>
      </template>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">接管 DNS</span>
            <span class="rs">由 mihomo 处理局域网域名解析，透明代理/TUN 模式建议开启<span class="rec">默认开启，建议保持</span></span>
          </div>
          <n-switch v-model:value="form.dns" />
        </div>
        <div class="row" v-if="form.dns">
          <div class="row-text">
            <span class="rt">DNS 解析模式</span>
            <span class="rs">fake-ip 返回假 IP（198.18.x.x），域名规则匹配最准；redir-host 返回真实 IP，兼容不支持假 IP 的设备（国外域名已自动经代理用国外 DNS 防污染复核）<span class="rec">默认 fake-ip，建议保持</span></span>
          </div>
          <n-select
            v-model:value="form.dns_mode"
            :options="[{ value: 'fake-ip', label: 'fake-ip' }, { value: 'redir-host', label: 'redir-host' }]"
            class="ctl"
            style="width: 190px"
          />
        </div>
        <div class="row" v-if="form.dns && store.status?.openwrt">
          <div class="row-text">
            <span class="rt">DNS 劫持模式</span>
            <span class="rs">防火墙转发强制接管所有设备的 DNS（包括手动改过 DNS 的设备）；dnsmasq 转发只覆盖使用路由器 DNS 的设备，设备自行配了 DNS 就会绕过内核（redir-host 下表现为部分网站打不开）<span class="rec">默认防火墙转发，建议保持</span></span>
          </div>
          <n-select
            v-model:value="form.dns_hijack"
            :options="[{ value: 'firewall', label: '防火墙转发' }, { value: 'dnsmasq', label: 'dnsmasq' }, { value: 'off', label: '禁用' }]"
            class="ctl"
            style="width: 190px"
          />
        </div>
        <div class="row" v-if="form.dns">
          <div class="row-text">
            <span class="rt">IPv4 劫持</span>
            <span class="rs">把局域网 IPv4 的 53 端口查询重定向到内核 DNS；防火墙转发与 TUN 模式均生效，保存后自动应用<span class="rec">默认开启，建议保持</span></span>
          </div>
          <n-switch v-model:value="form.dns_hijack_ipv4" />
        </div>
        <div class="row" v-if="form.dns">
          <div class="row-text">
            <span class="rt">IPv6 劫持</span>
            <span class="rs">同 IPv4 劫持，作用于 IPv6；不劫持时 IPv6 设备的域名解析走原路径，可能绕过内核<span class="rec">默认关闭；宽带无 IPv6 或无需接管时保持关闭</span></span>
          </div>
          <n-switch v-model:value="form.dns_hijack_ipv6" />
        </div>
      </div>
    </n-card>

    <!-- 内核 -->
    <n-card v-if="tab === 'core'" title="内核（mihomo）">
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">当前版本</span>
            <span class="rs mono">{{ coreInfo.path }}</span>
          </div>
          <span class="mono" :style="{ color: coreInfo.installed ? 'var(--green)' : 'var(--red)' }">
            {{ coreInfo.installed ? coreInfo.version || '已安装' : '未安装' }}
          </span>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">平台</span>
            <span class="rs">
              自动识别设备架构，无需配置<template v-if="coreInfo.platform">，当前为 <span class="mono">{{ coreInfo.platform }}</span></template><template v-else>，当前设备<span style="color: var(--orange)">不支持自动下载</span></template>
            </span>
          </div>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">内存限制</span>
            <span class="rs">内核接近上限时更积极回收内存（GOMEMLIMIT），设得过小会增加 CPU 开销换内存<span class="rec">默认 0 不限制；小内存设备建议 64～128</span></span>
          </div>
          <n-input-number v-model:value="form.core_mem_limit" :show-button="false" :min="0" :max="16384" class="num">
            <template #suffix><span class="unit">MB</span></template>
          </n-input-number>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">下载加速前缀</span>
            <span class="rs">内核/插件从 GitHub 下载时套用此前缀加速，直连 GitHub 够快可留空<span class="rec">默认 https://gh-proxy.com，建议保持</span></span>
          </div>
          <n-input v-model:value="form.download_proxy" placeholder="https://gh-proxy.com" style="width: 210px" />
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">更新内核</span>
            <span class="rs">
              <template v-if="coreLatest">
                最新 {{ coreLatest.latest }}
                <template v-if="coreLatest.has_update">（可更新）</template>
                <template v-else>（已是最新）</template>
              </template>
              <template v-else>从 GitHub 下载最新 mihomo</template>
              <span class="rec">建议：发现新版本再升级即可</span>
            </span>
          </div>
          <div class="btn-pair">
            <n-button size="small" :disabled="coreUpgrading" @click="checkCore">
              <template #icon><AppIcon name="search" :size="13" /></template>检查更新
            </n-button>
            <n-button v-if="coreLatest?.has_update" type="primary" size="small" :loading="coreUpgrading" @click="upgradeCore">
              <template #icon><AppIcon name="download" :size="13" /></template>{{ coreBtnText }}
            </n-button>
          </div>
        </div>
        <div class="row" v-if="coreUpgrading">
          <div class="row-text">
            <span class="rt">升级进度</span>
            <span class="rs mono">{{ progText || '正在连接…' }}</span>
          </div>
          <n-progress
            v-if="prog?.percent > 0"
            type="line"
            :percentage="prog.percent"
            :show-indicator="false"
            :height="6"
            border-radius="3px"
            class="prog"
          />
          <n-spin v-else :size="16" />
        </div>
      </div>
    </n-card>

    <!-- 插件 -->
    <n-card v-if="tab === 'plugin'" title="ClashV 插件">
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">更新插件</span>
            <span class="rs">
              <template v-if="pluginLatest">
                最新 {{ pluginLatest.latest }}
                <template v-if="pluginLatest.has_update">（可更新）</template>
                <template v-else>（已是最新）</template>
              </template>
              <template v-else>从 GitHub 下载最新版本</template>
              <span class="rec">建议：发现新版本再升级即可</span>
            </span>
          </div>
          <div class="btn-pair">
            <n-button size="small" :disabled="pluginUpgrading" @click="checkPlugin">
              <template #icon><AppIcon name="search" :size="13" /></template>检查更新
            </n-button>
            <n-button v-if="pluginLatest?.has_update" type="primary" size="small" :loading="pluginUpgrading" @click="upgradePlugin">
              <template #icon><AppIcon name="download" :size="13" /></template>{{ pluginBtnText }}
            </n-button>
          </div>
        </div>
        <div class="row" v-if="pluginUpgrading">
          <div class="row-text">
            <span class="rt">升级进度</span>
            <span class="rs mono">{{ progText || '正在连接…' }}</span>
          </div>
          <n-progress
            v-if="prog?.percent > 0"
            type="line"
            :percentage="prog.percent"
            :show-indicator="false"
            :height="6"
            border-radius="3px"
            class="prog"
          />
          <n-spin v-else :size="16" />
        </div>
        <div class="row" v-if="store.status?.openwrt">
          <div class="row-text">
            <span class="rt">重启服务</span>
            <span class="rs">修改令牌后需重启</span>
          </div>
          <n-button size="small" @click="restartService">
            <template #icon><AppIcon name="restart" :size="13" /></template>重启
          </n-button>
        </div>
      </div>
    </n-card>

    <!-- 备份与恢复：全部可持久化状态打包为 zip，支持多份与恢复 -->
    <n-card v-if="tab === 'plugin'" title="备份与恢复">
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">备份当前状态</span>
            <span class="rs">打包全部设置、订阅配置与列表、自定义规则和内核程序为一个备份文件（不含运行缓存与日志），支持创建多份</span>
          </div>
          <n-button size="small" type="primary" secondary @click="openBackupCreate">
            <template #icon><AppIcon name="save" :size="13" /></template>备份
          </n-button>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">备份列表</span>
            <span class="rs">查看已有备份，选择一份恢复或删除</span>
          </div>
          <n-button size="small" @click="openBackupList">
            <template #icon><AppIcon name="clock" :size="13" /></template>管理{{ backups.length ? `（${backups.length}）` : '' }}
          </n-button>
        </div>
      </div>
    </n-card>

    <!-- 恢复出厂：除订阅配置外全部数据回到安装初始状态，二次确认后执行 -->
    <n-card v-if="tab === 'plugin'">
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">重置所有配置</span>
            <span class="rs">停止内核，把设置、自定义规则、运行状态、日志全部恢复到插件安装时的初始状态；订阅配置（含当前激活项）与内核程序保留。操作不可恢复，需两次确认</span>
          </div>
          <n-button type="error" ghost size="small" :loading="resetting" @click="resetAll">
            <template #icon><AppIcon name="trash" :size="13" /></template>重置
          </n-button>
        </div>
      </div>
    </n-card>

    <!-- 创建备份弹窗：自动名可编辑 -->
    <n-modal
      preset="card" title="创建备份" :show="showBackupCreate"
      :style="{ width: '480px', maxWidth: '94vw' }"
      @update:show="showBackupCreate = false"
    >
      <div class="backup-body">
        <div class="page-sub">备份内容：全部设置、订阅配置与列表、自定义规则、内核程序（不含运行缓存与日志）</div>
        <n-input v-model:value="backupName" placeholder="备份名称（可编辑）" @keyup.enter="createBackup" />
        <div class="backup-foot">
          <span class="page-sub">同名备份会被拒绝，换个名字即可</span>
          <n-button type="primary" size="small" :loading="backupCreating" @click="createBackup">开始备份</n-button>
        </div>
      </div>
    </n-modal>

    <!-- 备份列表弹窗：恢复 / 删除 -->
    <n-modal
      preset="card" title="备份列表" :show="showBackupList"
      :style="{ width: '620px', maxWidth: '94vw' }"
      @update:show="showBackupList = false"
    >
      <n-spin :show="backupsLoading">
        <n-empty v-if="!backups.length" description="还没有备份，先创建一个吧" style="padding: 30px 0" />
        <div v-else class="backup-list">
          <div v-for="b in backups" :key="b.name" class="backup-row">
            <div class="row-text">
              <span class="rt mono">{{ b.name }}</span>
              <span class="rs">{{ fmtTime(b.created_at) }} · {{ fmtSize(b.size) }}</span>
            </div>
            <div class="btn-pair">
              <n-button size="small" type="primary" :loading="restoringName === b.name" @click="restoreBackup(b)">恢复</n-button>
              <n-button size="small" type="error" ghost :loading="deletingName === b.name" @click="deleteBackup(b)">删除</n-button>
            </div>
          </div>
        </div>
      </n-spin>
      <div class="backup-foot" style="margin-top: 10px">
        <span class="page-sub">恢复会覆盖当前全部设置、订阅与自定义规则；内核在运行时会自动重启</span>
      </div>
    </n-modal>

    <div class="save-bar" v-if="tab !== 'plugin'">
      <n-button type="primary" :loading="saving" :disabled="!loaded" @click="save">
        <template #icon><AppIcon name="save" :size="14" /></template>保存
      </n-button>
    </div>
  </div>
</template>

<style scoped>
.head-card :deep(.n-card-content) { padding: 10px 16px; }
/* segment tabs 自带 width:100% + tab 平分拉伸，这里收成内容宽度（与日志页同款不占满），窄屏兜底不溢出；
   naive segment tab 默认左右 padding 为 0（设计上靠拉伸撑宽），收窄后必须补横向内边距否则文字挤在一起 */
.tabs { width: fit-content; max-width: 100%; min-width: 0; }
.tabs :deep(.n-tabs-tab) { padding: 6px 16px; }
.rows { display: flex; flex-direction: column; }
/* 卡片标题副行（DNS 卡的「保持默认」提示）：与标题同一行、基线对齐，窄屏放不下时自动换行 */
.card-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.card-head .card-sub { color: var(--text-dim); font-size: 12px; font-weight: 400; line-height: 1.5; }
.row {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  padding: 11px 0;
  border-bottom: 1px solid var(--border);
}
.row:last-child { border-bottom: none; }
.row-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.rt { font-size: 13.5px; font-weight: 500; }
.rs { color: var(--text-dim); font-size: 12px; overflow: hidden; text-overflow: ellipsis; }
/* 行描述下方的「默认/建议」徽章：独立一行左对齐，主题色软底小胶囊 */
.rec {
  display: block;
  width: fit-content;
  max-width: 100%;
  margin-top: 3px;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 11px;
  line-height: 1.6;
}
.num { width: 110px; }
.unit { color: var(--text-dim); font-size: 12.5px; }
.btn-pair { display: flex; gap: 8px; flex: none; }
/* n-input-group 默认 flex 拉伸占满剩余宽度，收缩并靠右与行内其他控件一致 */
.row .n-input-group { width: fit-content; margin-left: auto; }
.prog { width: 180px; flex-shrink: 0; }
/* 常规流式布局：跟在卡片后面，不再悬浮遮挡内容 */
.save-bar { display: flex; justify-content: flex-end; padding: 2px 0 10px; }
/* 备份弹窗：内容列 + 弹窗底部说明/按钮行；列表行左右分布 */
.backup-body { display: flex; flex-direction: column; gap: 12px; }
.backup-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.backup-list { display: flex; flex-direction: column; gap: 8px; max-height: 50vh; overflow-y: auto; }
.backup-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 12px; border-radius: 10px; background: var(--bg-card-2); border: 1px solid var(--border); }

/* ---- 手机/平板：行内控件换到文案下方铺满，开关保持靠右 ---- */
@media (max-width: 760px) {
  .row { flex-wrap: wrap; }
  .row .ctl, .row .num, .row > .n-input { width: 100% !important; }
  .row > .n-switch { margin-left: auto; }
  .prog { width: 100%; }
}
</style>
