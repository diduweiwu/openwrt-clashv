<script setup>
// 设置页：基础设置、TUN/DNS、内核更新（mihomo）、插件更新
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { NButton, NCard, NEmpty, NInput, NInputGroup, NInputNumber, NModal, NProgress, NSelect, NSpin, NSwitch, NTabs, NTabPane } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask, fmtTime } from '../store.js'
import AppIcon from '../components/AppIcon.vue'
import HelpModal from '../components/HelpModal.vue'

// 设置项问号说明弹窗内容：k=小节标题（作用/开关影响/默认与建议/注意），v=正文。
// 行内不再放可见描述，全部收进弹窗，设置页只留名称+控件。
const HELP = {
  mixedPort: {
    title: '混合代理端口',
    rows: [
      { k: '作用', v: 'HTTP 与 SOCKS5 共用的代理端口，局域网设备手动配置代理时填这个端口。' },
      { k: '默认与建议', v: '默认 7890，建议保持；端口被占用再改。' },
    ],
  },
  token: {
    title: '界面访问令牌',
    rows: [
      { k: '作用', v: '保护管理界面：设置后，局域网内其他设备打开本页面需要先输入令牌。' },
      { k: '默认与建议', v: '默认不启用；局域网内使用建议设置一个，仅本机访问可留空。' },
      { k: '小提示', v: '右侧闪电按钮可随机生成 32 位令牌，垃圾桶按钮一键清空（即停用）。' },
    ],
  },
  luciAuth: {
    title: 'OpenWrt 登录校验',
    rows: [
      { k: '作用', v: '开启后必须先登录 OpenWrt 才能使用本界面：从 LuCI 菜单进入会自动放行；直接访问 路由器IP:9097 会自动跳转到 LuCI 的 ClashV 页（未登录 LuCI 时就是登录页）。' },
      { k: '开关影响', v: '退出 OpenWrt 登录后界面立即失效；关闭校验则知道地址的任何人都能打开界面。' },
      { k: '默认与建议', v: '默认开启，建议保持；本机访问与界面访问令牌不受影响。' },
    ],
  },
  downloadProxy: {
    title: '下载加速前缀',
    rows: [
      { k: '作用', v: '内核/插件从 GitHub 下载时套用此前缀加速。' },
      { k: '默认与建议', v: '默认 https://gh-proxy.com，建议保持；直连 GitHub 够快可留空。' },
      { k: '注意', v: '对私有仓库无效，私有仓库下载将直连 GitHub。' },
    ],
  },
  tun: {
    title: 'TUN 模式',
    rows: [
      { k: '作用', v: '创建虚拟网卡接管全局流量（含 UDP），需要内核 tun 模块。' },
      { k: '开关影响', v: '开启后全局流量（含 UDP）都经内核处理；关闭时自动改用防火墙接管局域网 TCP（透明代理），无需手动配置。' },
      { k: '默认与建议', v: '默认关闭，建议保持；需要接管 UDP/全局流量时再开。' },
    ],
  },
  tunStack: {
    title: 'TUN 协议栈',
    rows: [
      { k: '作用', v: '选择 TUN 模式的网络栈实现：system 走系统网络栈，gvisor 为纯用户态实现，mixed 混合两者。' },
      { k: '默认与建议', v: '默认 mixed，建议保持。' },
    ],
  },
  dns: {
    title: '接管 DNS',
    rows: [
      { k: '作用', v: '由 mihomo 内核处理局域网域名解析。' },
      { k: '开关影响', v: '关闭后下方所有 DNS 选项都不再生效，域名解析走路由器原路径。' },
      { k: '默认与建议', v: '默认开启，建议保持；透明代理/TUN 模式下建议开启。' },
    ],
  },
  dnsMode: {
    title: 'DNS 解析模式',
    rows: [
      { k: '作用', v: 'fake-ip 返回假 IP（198.18.x.x），域名规则匹配最准；redir-host 返回真实 IP，兼容不支持假 IP 的设备。' },
      { k: '说明', v: '国外域名已自动经代理用国外 DNS 防污染复核。' },
      { k: '默认与建议', v: '默认 fake-ip，建议保持。' },
    ],
  },
  dnsHijack: {
    title: 'DNS 劫持模式',
    rows: [
      { k: '作用', v: '决定局域网设备的 DNS 查询如何进入内核。防火墙转发：强制接管所有设备的 DNS（包括手动改过 DNS 的设备）；dnsmasq：只覆盖使用路由器 DNS 的设备，设备自行配了 DNS 就会绕过内核（redir-host 下表现为部分网站打不开）；禁用：不做劫持。' },
      { k: '默认与建议', v: '默认防火墙转发，建议保持。' },
    ],
  },
  dnsV4: {
    title: 'IPv4 劫持',
    rows: [
      { k: '作用', v: '把局域网 IPv4 的 53 端口 DNS 查询重定向到内核；防火墙转发与 TUN 模式均生效，保存后自动应用。' },
      { k: '默认与建议', v: '默认开启，建议保持。' },
    ],
  },
  dnsV6: {
    title: 'IPv6 劫持',
    rows: [
      { k: '作用', v: '同 IPv4 劫持，作用于 IPv6。' },
      { k: '开关影响', v: '不劫持时 IPv6 设备的域名解析走原路径，可能绕过内核。' },
      { k: '默认与建议', v: '默认关闭；宽带无 IPv6 或无需接管时保持关闭。' },
    ],
  },
  platform: {
    title: '平台',
    rows: [
      { k: '作用', v: '自动识别设备架构，无需配置；下载内核时按此平台名匹配对应文件。' },
      { k: '说明', v: '绿色表示识别成功；橙色「不支持自动下载」表示当前架构没有对应内核包，需手动放置内核文件。' },
    ],
  },
  memLimit: {
    title: '内存限制',
    rows: [
      { k: '作用', v: '内核内存接近上限时更积极地回收内存（GOMEMLIMIT）。' },
      { k: '开关影响', v: '设得过小会用 CPU 开销换内存（回收更频繁）；0 表示不限制。' },
      { k: '默认与建议', v: '默认 0 不限制；小内存设备建议 64～128。' },
    ],
  },
  coreUpdate: {
    title: '更新内核',
    rows: [
      { k: '作用', v: '从 GitHub 下载最新版 mihomo 内核并安装。' },
      { k: '默认与建议', v: '建议：发现新版本再升级即可。' },
    ],
  },
  pluginVersion: {
    title: '当前版本',
    rows: [
      { k: '作用', v: '显示正在运行的插件版本；「检查更新」拿它与在线仓库的最新版比较。' },
    ],
  },
  pluginRepo: {
    title: '在线仓库地址',
    rows: [
      { k: '作用', v: 'GitHub 仓库（owner/repo），检查更新与下载安装包都从这里获取。' },
      { k: '默认与建议', v: '默认 diduweiwu/openwrt-clashv。' },
    ],
  },
  githubToken: {
    title: 'GitHub 访问令牌',
    rows: [
      { k: '作用', v: '仓库为私有时必填：查询 Release 与下载安装包会携带此令牌，需要该仓库的读取权限（classic token 勾选 repo，fine-grained token 勾选 Contents 只读）。' },
      { k: '注意', v: '下载加速前缀对私有仓库无效，将直连 GitHub。' },
      { k: '默认与建议', v: '公开仓库留空即可；私有仓库不填会报「仓库不存在」。' },
    ],
  },
  pluginUpdate: {
    title: '更新插件',
    rows: [
      { k: '作用', v: '从在线仓库获取最新 Release，自动匹配设备架构的 ipk/apk 安装包并安装，安装完成后服务自动重启。' },
      { k: '默认与建议', v: '建议：发现新版本再升级即可。' },
    ],
  },
  restart: {
    title: '重启服务',
    rows: [
      { k: '作用', v: '重启 ClashV 插件服务进程；修改界面访问令牌后需重启才能生效。' },
    ],
  },
  backupCreate: {
    title: '备份当前状态',
    rows: [
      { k: '作用', v: '把全部设置、订阅配置与列表、自定义规则和内核程序打包为一个备份文件（不含运行缓存与日志），支持创建多份。' },
    ],
  },
  backupList: {
    title: '备份列表',
    rows: [
      { k: '作用', v: '查看已有备份，可选择一份恢复或删除。' },
      { k: '注意', v: '恢复会覆盖当前全部设置、订阅与自定义规则；内核在运行时会自动重启。' },
    ],
  },
  reset: {
    title: '重置所有配置',
    rows: [
      { k: '作用', v: '停止内核，把设置、自定义规则、运行状态、日志全部恢复到插件安装时的初始状态；订阅配置（含当前激活项）与内核程序保留。' },
      { k: '注意', v: '操作不可恢复，需两次确认。' },
    ],
  },
}

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
  luci_auth: true,
  core_arch: '',
  core_mem_limit: 0,
  download_proxy: 'https://gh-proxy.com',
  plugin_repo: 'diduweiwu/openwrt-clashv',
  github_token: '',
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
      luci_auth: s.luci_auth !== false,
      core_arch: s.core_arch || '',
      core_mem_limit: s.core_mem_limit || 0,
      download_proxy: s.download_proxy || '',
      plugin_repo: s.plugin_repo || '',
      github_token: s.github_token || '',
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
      luci_auth: r.settings.luci_auth !== false,
      core_arch: r.settings.core_arch || '',
      core_mem_limit: r.settings.core_mem_limit || 0,
      download_proxy: r.settings.download_proxy || '',
      plugin_repo: r.settings.plugin_repo || '',
      github_token: r.settings.github_token || '',
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
  const p = pluginLatest.value?.pkg
  const pkgText = p ? `匹配包 ${p.name}（${p.format}），` : ''
  if (!(await ask('更新插件', `确认下载并安装插件最新版本？${pkgText}安装完成后服务会自动重启，期间界面会短暂失联`))) return
  pluginUpgrading.value = true
  startProgPoll()
  try {
    const r = await api.post('/api/plugin/upgrade')
    if (r.need_restart) {
      // 非 OpenWrt 裸二进制流程：已替换自身，需手动重启
      toast('插件已更新，请手动重启服务生效', 'success')
    } else {
      await waitPluginInstalled()
    }
  } catch (e) {
    toast(e.message, 'error', 6000)
  } finally {
    stopProgPoll()
    pluginUpgrading.value = false
  }
}

// OpenWrt 包流程：升级接口返回即安装包已下载到路由器，安装与服务重启在后台
// 进行。服务失联再恢复 = 新版本已上线，自动刷新加载新界面；服务一直在线则
// 从升级进度里读安装结果（依赖缺失等失败会记录在那里）。
async function waitPluginInstalled() {
  toast('安装包已下载，正在安装，服务将自动重启…', 'success', 6000)
  const deadline = Date.now() + 180000
  let downSeen = false
  while (Date.now() < deadline) {
    await new Promise(r => setTimeout(r, 2000))
    if (prog.value?.stage === 'error') { toast(prog.value.message, 'error', 8000); return }
    if (prog.value?.stage === 'done') { toast('安装完成，但服务未自动重启（可能未开机自启），请手动重启生效', 'success', 8000); return }
    try {
      await api.get('/api/status')
      if (downSeen) {
        toast('服务已重启，正在加载新版本…', 'success')
        setTimeout(() => location.reload(), 1200)
        return
      }
    } catch { downSeen = true }
  }
  toast('等待服务重启超时，请稍后刷新页面确认版本', 'error', 8000)
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

// 设置分区 tab：通用（代理基础+访问控制+下载加速）/ 网络 / 内核 / 插件
const tab = ref('general')
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h2 class="page-title">设置</h2>
        <p class="page-sub">插件全部配置 · 修改后点底部「保存」生效，涉及连接的改动会自动重启内核</p>
      </div>
      <n-tabs v-model:value="tab" type="segment" size="small" class="tabs">
        <n-tab-pane name="general"><template #tab>通用</template></n-tab-pane>
        <n-tab-pane name="network"><template #tab>网络</template></n-tab-pane>
        <n-tab-pane name="core"><template #tab>内核</template></n-tab-pane>
        <n-tab-pane name="plugin"><template #tab>插件</template></n-tab-pane>
      </n-tabs>
    </div>

    <!-- 通用 -->
    <template v-if="tab === 'general'">
      <n-card title="代理基础">
        <div class="rows">
          <div class="row">
            <div class="row-text">
              <span class="rt">混合代理端口<HelpModal v-bind="HELP.mixedPort" /></span>
            </div>
            <n-input-number v-model:value="form.mixed_port" :min="1" :max="65535" class="num" />
          </div>
          <div class="row">
            <div class="row-text">
              <span class="rt">界面访问令牌<HelpModal v-bind="HELP.token" /></span>
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
          <div class="row" v-if="store.status?.openwrt">
            <div class="row-text">
              <span class="rt">OpenWrt 登录校验<HelpModal v-bind="HELP.luciAuth" /></span>
            </div>
            <n-switch v-model:value="form.luci_auth" />
          </div>
          <div class="row">
            <div class="row-text">
              <span class="rt">下载加速前缀<HelpModal v-bind="HELP.downloadProxy" /></span>
            </div>
            <n-input v-model:value="form.download_proxy" placeholder="https://gh-proxy.com" style="width: 210px" />
          </div>
        </div>
      </n-card>
    </template>

    <!-- 网络：TUN 与 DNS 拆成两张卡，互不混淆 -->
    <n-card v-if="tab === 'network'" title="TUN">
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">TUN 模式<HelpModal v-bind="HELP.tun" /></span>
          </div>
          <n-switch v-model:value="form.tun" />
        </div>
        <div class="row" v-if="form.tun">
          <div class="row-text">
            <span class="rt">TUN 协议栈<HelpModal v-bind="HELP.tunStack" /></span>
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
            <span class="rt">接管 DNS<HelpModal v-bind="HELP.dns" /></span>
          </div>
          <n-switch v-model:value="form.dns" />
        </div>
        <div class="row" v-if="form.dns">
          <div class="row-text">
            <span class="rt">DNS 解析模式<HelpModal v-bind="HELP.dnsMode" /></span>
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
            <span class="rt">DNS 劫持模式<HelpModal v-bind="HELP.dnsHijack" /></span>
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
            <span class="rt">IPv4 劫持<HelpModal v-bind="HELP.dnsV4" /></span>
          </div>
          <n-switch v-model:value="form.dns_hijack_ipv4" />
        </div>
        <div class="row" v-if="form.dns">
          <div class="row-text">
            <span class="rt">IPv6 劫持<HelpModal v-bind="HELP.dnsV6" /></span>
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
            <span class="rt">平台<HelpModal v-bind="HELP.platform" /></span>
          </div>
          <span class="mono" :style="{ color: coreInfo.platform ? 'var(--green)' : 'var(--orange)' }">
            {{ coreInfo.platform || '不支持自动下载' }}
          </span>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">内存限制<HelpModal v-bind="HELP.memLimit" /></span>
          </div>
          <n-input-number v-model:value="form.core_mem_limit" :show-button="false" :min="0" :max="16384" class="num">
            <template #suffix><span class="unit">MB</span></template>
          </n-input-number>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">更新内核<HelpModal v-bind="HELP.coreUpdate" /></span>
            <span class="rs" v-if="coreLatest">
              最新 {{ coreLatest.latest }}
              <template v-if="coreLatest.has_update">（可更新）</template>
              <template v-else>（已是最新）</template>
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
            <span class="rt">当前版本<HelpModal v-bind="HELP.pluginVersion" /></span>
          </div>
          <span class="mono">{{ store.status?.plugin_version === 'dev' ? '开发版' : store.status?.plugin_version ? 'v' + store.status.plugin_version : '…' }}</span>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">在线仓库地址<HelpModal v-bind="HELP.pluginRepo" /></span>
          </div>
          <n-input v-model:value="form.plugin_repo" placeholder="owner/repo" style="width: 210px" />
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">GitHub 访问令牌<HelpModal v-bind="HELP.githubToken" /></span>
          </div>
          <n-input
            v-model:value="form.github_token"
            type="password"
            show-password-on="click"
            placeholder="ghp_… / github_pat_…，留空不启用"
            style="width: 210px"
          />
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">更新插件<HelpModal v-bind="HELP.pluginUpdate" /></span>
            <span class="rs" v-if="pluginLatest">
              最新 {{ pluginLatest.latest }}
              <template v-if="pluginLatest.pkg"> · 匹配包 {{ pluginLatest.pkg.name }}（{{ pluginLatest.pkg.format }}）</template>
              <template v-if="pluginLatest.has_update">（可更新）</template>
              <template v-else>（已是最新）</template>
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
            <span class="rt">重启服务<HelpModal v-bind="HELP.restart" /></span>
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
            <span class="rt">备份当前状态<HelpModal v-bind="HELP.backupCreate" /></span>
          </div>
          <n-button size="small" type="primary" secondary @click="openBackupCreate">
            <template #icon><AppIcon name="save" :size="13" /></template>备份
          </n-button>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">备份列表<HelpModal v-bind="HELP.backupList" /></span>
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
            <span class="rt">重置所有配置<HelpModal v-bind="HELP.reset" /></span>
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

    <!-- 插件 tab 现在含在线仓库地址等可保存项，保存栏全 tab 显示 -->
    <div class="save-bar">
      <n-button type="primary" :loading="saving" :disabled="!loaded" @click="save">
        <template #icon><AppIcon name="save" :size="14" /></template>保存
      </n-button>
    </div>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
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
.rt { display: flex; align-items: center; gap: 4px; font-size: 13.5px; font-weight: 500; }
.rs { color: var(--text-dim); font-size: 12px; overflow: hidden; text-overflow: ellipsis; }
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
