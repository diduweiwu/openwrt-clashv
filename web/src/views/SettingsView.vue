<script setup>
// 设置页：基础设置、TUN/DNS、内核更新（mihomo）、插件更新
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { NButton, NCard, NEmpty, NInput, NInputGroup, NInputNumber, NModal, NProgress, NSelect, NSlider, NSpin, NSwitch, NTabs, NTabPane, NTag } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask, fmtTime } from '../store.js'
import { glassEnabled, glassOpacity } from '../glass.js'
import { menuMode } from '../menuMode.js'
import AppIcon from '../components/AppIcon.vue'
import HelpModal from '../components/HelpModal.vue'
import UpdateModal from '../components/UpdateModal.vue'

// 设置项问号说明弹窗内容：k=小节标题（作用/开关影响/默认与建议/注意），v=正文。
// 行内不再放可见描述，全部收进弹窗，设置页只留名称+控件。
const HELP = {
  menuMode: {
    title: '菜单模式',
    rows: [
      { k: '作用', v: '选择桌面端的菜单布局：左侧竖向为经典侧栏（默认），顶部横向把导航收成页面顶部一条横栏，内容区更宽、更利落。' },
      { k: '注意', v: '仅桌面端生效；手机窄屏固定为顶部横向菜单，不受此设置影响。改动即时生效并保存在本浏览器（不占用后端设置）。' },
    ],
  },
  glass: {
    title: '磨砂模糊',
    rows: [
      { k: '作用', v: '开启后卡片、侧边栏等界面元素改为半透明底 + 背景磨砂模糊（毛玻璃），页面同时衬两个极淡的光斑让效果可见；透明度可用下方滑杆调节。' },
      { k: '提示', v: '改动即时生效并保存在本浏览器（不占用后端设置）；背景模糊由浏览器渲染，低性能设备如感觉滚动卡顿可关闭。' },
      { k: '默认与建议', v: '默认关闭；透明度默认 45%，滑杆已限制调节范围，不会透到文字看不清。' },
    ],
  },
  glassOpacity: {
    title: '磨砂透明度',
    rows: [
      { k: '作用', v: '调节磨砂开启时卡片与侧栏背景的透明程度：数值越大越透，越小越接近实心。' },
      { k: '范围', v: '10%（近乎实心）～ 100%（全透，只留磨砂模糊）；默认 45%。字体颜色不随透明度变化，拉满后靠磨砂模糊与背景光晕衬托，仍能保持文字可读。' },
    ],
  },
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
  autoReboot: {
    title: '断网自动重启',
    rows: [
      { k: '作用', v: '开启后插件每 30 秒经内核代理访问一次检测地址：地址访问不通（超时或返回异常状态码）时，自动重启内核尝试恢复。' },
      { k: '次数限制', v: '连续失败期间最多自动重启「重启次数上限」次，防止无限重启；期间任意一次检测恢复连通，计数即清零重新开始。' },
      { k: '默认与建议', v: '默认关闭，检测地址默认 google.com；内核偶尔假死需要手动重启才能恢复的，建议开启。' },
    ],
  },
  autoRebootURL: {
    title: '检测地址',
    rows: [
      { k: '作用', v: '用于判断「代理能否上网」的地址，插件定时经内核代理 GET 访问它：返回 2xx/3xx 视为正常，超时（10 秒）或 4xx/5xx 视为不通。' },
      { k: '建议', v: '默认 google.com，代理正常时能打开就说明链路健康；也可换成机场订阅地址等任意轻量网址。' },
      { k: '注意', v: '检测请求经内核代理发出（本机直连不走内核，测不了代理链路），所以需要内核运行中才能配置与验证；右侧「检测」按钮可随时手动试。' },
    ],
  },
  autoRebootMax: {
    title: '重启次数上限',
    rows: [
      { k: '作用', v: '同一个故障周期内（连续检测失败期间）最多自动重启内核的次数，1～10 次，默认 1。' },
      { k: '举例', v: '设为 2：检测不通重启一次，30 秒后再不通再重启一次，之后仍不通就不再重启，直到某次检测恢复连通才重新计数。' },
    ],
  },
  coreChannel: {
    title: '内核渠道',
    rows: [
      { k: '作用', v: '选择内核的发布渠道。正式版（Release）：mihomo 官方稳定版本，推荐日常使用；抢先版（Alpha）：跟随 mihomo 开发分支构建，新特性与修复先到，但可能不稳定。' },
      { k: '切换行为', v: '两渠道内核独立存放、互不覆盖：目标渠道已有内核时，保存后自动重启内核切换；还没有时会提示先下载，下载完成自动重启。' },
    ],
  },
  platform: {
    title: '平台',
    rows: [
      { k: '作用', v: '自动识别设备架构，无需配置；下载内核时按此平台名匹配对应文件。' },
      { k: '说明', v: '绿色表示识别成功；橙色「不支持自动下载」表示当前架构没有对应内核包，可用下方「手动上传内核」自行安装。' },
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
      { k: '作用', v: '从 GitHub 下载当前渠道最新版 mihomo 内核并安装；抢先版（Alpha）更新频繁，可随时升级到最新构建。' },
      { k: '默认与建议', v: '建议：发现新版本再升级即可。' },
    ],
  },
  coreUpload: {
    title: '手动上传内核',
    rows: [
      { k: '作用', v: '从本地文件安装内核：支持原始二进制或 mihomo Release 的 .gz 压缩包（自动识别解压）。上传后自动重命名为当前渠道的内核文件名、添加执行权限并试运行校验（要求能在本设备执行且确为 mihomo），全部通过才替换现有内核；内核在运行时会自动重启加载。' },
      { k: '渠道归属', v: '安装到哪个渠道跟随上方「内核渠道」设置；上传文件本身的渠道不做校验——把正式版内核装进 Alpha 渠道也不会被拦截。' },
      { k: '安全性', v: '校验失败（架构不符、文件损坏、不是 mihomo）只清理临时文件，现有内核不受影响。' },
    ],
  },
  pluginVersion: {
    title: '当前版本',
    rows: [
      { k: '作用', v: '显示正在运行的插件版本；「检查」拿它与在线仓库的最新版比较。' },
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
  auto_reboot_enabled: false,
  auto_reboot_url: '',
  auto_reboot_max: 1,
  token: '',
  luci_auth: true,
  core_arch: '',
  core_channel: 'release',
  core_mem_limit: 0,
  download_proxy: 'https://gh-proxy.com',
  plugin_repo: 'diduweiwu/openwrt-clashv',
  github_token: '',
})
const loaded = ref(false)
const saving = ref(false)

const coreInfo = ref({ installed: false, version: '', path: '', platform: '', channel: '' })
const coreLatest = ref(null) // {current, latest, notes, has_update}
// 升级确认弹窗（UpdateModal）状态：{kind, title, latest, current, notes, pkgText}，null=关闭
const updateAsk = ref(null)
// v-model:show 需要可写的 getter/setter：null 即关闭
const updateAskShow = computed({
  get: () => !!updateAsk.value,
  set: (v) => { if (!v) updateAsk.value = null },
})

// 弹窗确认回调：按弹窗记录的类别发起对应升级流程
function confirmUpdate() {
  const kind = updateAsk.value?.kind
  updateAsk.value = null
  if (kind === 'plugin') doUpgradePlugin()
  else if (kind === 'core') downloadCore()
}
const coreUpgrading = ref(false)
const pluginLatest = ref(null)
const pluginUpgrading = ref(false)

// 标记取消是用户自己点的：升级接口会以「已取消」失败、恢复轮询也会拿到
// error 终态，两处都靠它跳过错误 toast
const upgradeCancelled = ref(false)

// 取消进行中的升级任务（内核/插件共用同一取消入口）
async function cancelUpgrade() {
  upgradeCancelled.value = true
  try { await api.post('/api/upgrade/cancel') } catch { /* 任务可能刚自行结束 */ }
}

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

// resumeKind 非空 = 页面刷新后重连已在上跑的任务：任务进入终态时在这里收尾。
// 正常发起的升级由升级接口自身的返回收尾（resumeKind 为空），不走这里防重复提示。
function startProgPoll(resumeKind = '') {
  stopProgPoll()
  prog.value = null
  const tick = async () => {
    try {
      prog.value = await api.get('/api/upgrade/progress')
    } catch { /* 轮询失败下次再试 */ return }
    if (!(resumeKind && prog.value && !prog.value.active)) return
    const { stage, message } = prog.value
    stopProgPoll()
    if (resumeKind === 'core') {
      coreUpgrading.value = false
      if (stage === 'done') toast(message || '内核已更新', 'success', 5000)
      else if (stage === 'error' && !upgradeCancelled.value) toast(message || '内核更新失败', 'error', 6000)
      try {
        coreInfo.value = await api.get('/api/core/status')
        store.status = await api.get('/api/status')
      } catch { /* 状态刷新失败不影响收尾 */ }
    } else if (resumeKind === 'plugin') {
      pluginUpgrading.value = false
      if (stage === 'done') toast(message || '插件已更新', 'success', 6000)
      else if (stage === 'error' && !upgradeCancelled.value) toast(message || '插件更新失败', 'error', 6000)
    }
  }
  tick()
  progTimer = setInterval(tick, 600)
}
function stopProgPoll() {
  if (progTimer) { clearInterval(progTimer); progTimer = null }
}

// 周期感知进行中的升级任务：首页等别处发起的下载卡住时，切到设置页也能看到
// 「升级进度」行（含取消按钮），不会因为本地 upgrading 标志未置位而无从中止。
// 本地已在跟进（coreUpgrading/pluginUpgrading）时跳过，避免与进度轮询重复。
async function senseUpgradeTask() {
  if (coreUpgrading.value || pluginUpgrading.value) return
  try {
    const p = await api.get('/api/upgrade/progress')
    if (!p?.active) return
    if (p.kind === 'core') { coreUpgrading.value = true; startProgPoll('core') }
    else if (p.kind === 'plugin') { pluginUpgrading.value = true; startProgPoll('plugin') }
  } catch { /* 忽略，下轮再感知 */ }
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
      auto_reboot_enabled: !!s.auto_reboot_enabled,
      auto_reboot_url: s.auto_reboot_url || '',
      auto_reboot_max: s.auto_reboot_max || 1,
      token: s.token || '',
      luci_auth: s.luci_auth !== false,
      core_arch: s.core_arch || '',
      core_channel: s.core_channel || 'release',
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

// ---- 断网检测地址验证：手动「检测」按钮与保存后自动验证共用 ----
const watchTesting = ref(false)

// testWatchUrl 调后端经内核代理拨测一次地址，结果用 toast 反馈。
// 后端会自动补 http:// 前缀，与保存时的 normalize 行为一致。
async function testWatchUrl() {
  if (watchTesting.value) return
  watchTesting.value = true
  try {
    const r = await api.post('/api/watch/test', { url: form.auto_reboot_url })
    if (r.ok) toast(`检测地址可用（${r.ms} ms）`, 'success')
    else toast(r.error || '检测地址当前不可访问', 'warning', 6000)
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    watchTesting.value = false
  }
}

// verifyWatchUrlAfterSave 保存成功后的自动验证：开关开着、地址非空且内核
// 运行中才验（验证走内核代理）。验证结果只提示不拦保存——地址暂时不通
// 不妨碍先把配置存上，用户可稍后手动再测。
async function verifyWatchUrlAfterSave() {
  if (!form.auto_reboot_enabled || !form.auto_reboot_url.trim() || !store.status?.running) return
  try {
    const r = await api.post('/api/watch/test', { url: form.auto_reboot_url })
    if (r.ok) toast(`检测地址验证通过（${r.ms} ms）`, 'success', 4000)
    else toast('检测地址当前不可访问，已保存配置，可稍后用「检测」再试', 'warning', 6000)
  } catch { /* 验证失败不影响保存结果 */ }
}

async function save() {
  saving.value = true
  try {
    // 合并回完整设置（保留后端管理的字段）；数字输入清空时兜底默认值
    const cur = await api.get('/api/settings')
    // 保存前记录断网守护的现值：开关与地址都没动就不必在保存后拨测验证
    const prevWatchEnabled = !!cur.auto_reboot_enabled
    const prevWatchUrl = cur.auto_reboot_url || ''
    const payload = {
      ...cur,
      ...form,
      mixed_port: Number(form.mixed_port) || 7890,
      ui_port: Number(form.ui_port) || 9097,
      core_mem_limit: Math.max(0, Number(form.core_mem_limit) || 0),
      auto_reboot_max: Math.min(10, Math.max(1, Number(form.auto_reboot_max) || 1)),
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
      auto_reboot_enabled: !!r.settings.auto_reboot_enabled,
      auto_reboot_url: r.settings.auto_reboot_url || '',
      auto_reboot_max: r.settings.auto_reboot_max || 1,
      token: r.settings.token || '',
      luci_auth: r.settings.luci_auth !== false,
      core_arch: r.settings.core_arch || '',
      core_channel: r.settings.core_channel || 'release',
      core_mem_limit: r.settings.core_mem_limit || 0,
      download_proxy: r.settings.download_proxy || '',
      plugin_repo: r.settings.plugin_repo || '',
      github_token: r.settings.github_token || '',
    })
    if (r.error) toast('已保存，但内核重启失败：' + r.error, 'error')
    else if (r.restarted) toast('已保存，内核已重载生效', 'success')
    else toast('已保存' + (r.need_reload ? '（令牌需重启服务后生效）' : ''), 'success')
    // 切换内核渠道后目标渠道还没有内核：引导立即下载（下载完成且内核在运行会自动重启到新渠道）
    if (r.core_channel_missing) {
      const label = r.settings.core_channel === 'alpha' ? '抢先版（Alpha）' : '正式版（Release）'
      if (await ask('下载内核', `已切换到${label}渠道，本地还没有该渠道的内核文件，是否立即下载？`)) {
        await downloadCore()
      }
    }
    // 渠道切换会换内核文件与重启，同步刷新内核状态展示
    try { coreInfo.value = await api.get('/api/core/status') } catch { /* 状态刷新失败不影响保存结果 */ }
    store.status = await api.get('/api/status')
    // 断网守护的开关或地址有变化时才顺带验证检测地址（经内核代理），
    // 没改就不必每次保存都拨测；结果只提示不拦保存
    if (prevWatchEnabled !== !!r.settings.auto_reboot_enabled
      || prevWatchUrl !== (r.settings.auto_reboot_url || '')) {
      await verifyWatchUrlAfterSave()
    }
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

// 发起内核下载安装（渠道跟随当前设置），确认弹窗由调用方决定是否先出
async function downloadCore() {
  coreUpgrading.value = true
  upgradeCancelled.value = false
  startProgPoll()
  try {
    const r = await api.post('/api/core/upgrade')
    toast(`内核已更新到 ${r.version}`, 'success', 5000)
    coreInfo.value = await api.get('/api/core/status')
    coreLatest.value = null
    store.status = await api.get('/api/status')
  } catch (e) {
    if (!upgradeCancelled.value) toast(e.message, 'error', 6000)
  } finally {
    stopProgPoll()
    coreUpgrading.value = false
  }
}

// 内核升级入口：弹升级确认弹窗（新版本徽标 + mihomo Release 更新日志），确认后下载
function upgradeCore() {
  updateAsk.value = {
    kind: 'core',
    title: '升级内核',
    latest: coreLatest.value?.latest,
    current: coreLatest.value?.current || coreInfo.value?.version || '',
    notes: coreLatest.value?.notes || '',
    pkgText: '确认后下载并安装，视网络情况可能需要几分钟',
  }
}

// ---- 手动上传内核：选文件 → 确认 → 上传（后端校验通过才替换当前渠道内核）----
const coreUploading = ref(false)
const coreFileInput = ref(null)

function pickCoreFile() {
  coreFileInput.value?.click()
}

// 选中文件后确认并上传；清空 value 让同一个文件可以重复选择。
// 安装到哪个渠道跟随已保存的「内核渠道」设置——表单里改了还没保存时先拦下，
// 提示先保存，避免界面显示的渠道和实际安装的渠道对不上。
async function onCoreFilePicked(e) {
  const file = e.target.files?.[0]
  e.target.value = ''
  if (!file) return
  if (coreInfo.value.channel && form.core_channel !== coreInfo.value.channel) {
    toast('内核渠道已改动但尚未保存，请先点底部「保存」再上传', 'error', 6000)
    return
  }
  const label = form.core_channel === 'alpha' ? '抢先版（Alpha）' : '正式版（Release）'
  const running = store.status?.running
  if (!(await ask('上传内核', `将把「${file.name}」安装为${label}渠道内核：校验通过才会替换现有内核${running ? '，内核会自动重启' : ''}。确定上传？`))) return
  coreUploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', file)
    const r = await api.upload('/api/core/upload', fd)
    toast(`内核已安装 ${r.version}（${label}）`, 'success', 5000)
    coreInfo.value = await api.get('/api/core/status')
    coreLatest.value = null
    store.status = await api.get('/api/status')
  } catch (e2) {
    toast(e2.message, 'error', 6000)
  } finally {
    coreUploading.value = false
  }
}

async function checkPlugin() {
  try {
    pluginLatest.value = await api.get('/api/plugin/latest')
  } catch (e) {
    toast(e.message, 'error')
  }
}

// 插件升级入口：弹升级确认弹窗（新版本徽标 + Release 更新日志），确认后下载安装
function upgradePlugin() {
  const p = pluginLatest.value?.pkg
  updateAsk.value = {
    kind: 'plugin',
    title: '更新插件',
    latest: pluginLatest.value?.latest,
    current: pluginLatest.value?.current
      ? (pluginLatest.value.current === 'dev' ? '开发版' : 'v' + pluginLatest.value.current)
      : '',
    notes: pluginLatest.value?.notes || '',
    pkgText: p
      ? `将下载匹配包 ${p.name}（${p.format}）；安装完成后服务会自动重启，期间界面会短暂失联`
      : '安装完成后服务会自动重启，期间界面会短暂失联',
  }
}

// 弹窗确认后的实际安装流程（原 upgradePlugin 的下载安装部分）
async function doUpgradePlugin() {
  pluginUpgrading.value = true
  upgradeCancelled.value = false
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
    if (!upgradeCancelled.value) toast(e.message, 'error', 6000)
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

let senseTimer = null
onMounted(() => {
  load(); refreshBackups()
  senseUpgradeTask()
  senseTimer = setInterval(senseUpgradeTask, 3000)
})
onBeforeUnmount(() => { stopProgPoll(); if (senseTimer) { clearInterval(senseTimer); senseTimer = null } })

// 设置分区 tab：通用（代理基础+访问控制+下载加速）/ 订阅（配置模板）/ 网络 / 内核 / 插件
const tab = ref('general')

// ---- 配置模板（订阅 tab）：手动节点方式添加订阅时注入 proxies 与 __ALL_PROXIES__ ----
const templates = ref([])
const tplLoading = ref(false)
const showTplModal = ref(false)
const tplEditing = ref(null) // 正在编辑/查看的模板元数据；null=新建
const tplForm = reactive({ name: '', content: '' })
const tplSaving = ref(false)
const tplDeleting = ref('')

async function loadTemplates() {
  tplLoading.value = true
  try {
    const r = await api.get('/api/templates')
    templates.value = r.templates || []
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    tplLoading.value = false
  }
}
// 首次切到订阅 tab 再拉取，避免多余的启动请求
watch(tab, v => { if (v === 'subs' && !templates.value.length) loadTemplates() })

function openTplNew() {
  tplEditing.value = null
  tplForm.name = ''
  tplForm.content = ''
  showTplModal.value = true
}

async function openTplEdit(t) {
  try {
    const r = await api.get(`/api/templates/${encodeURIComponent(t.name)}/content`)
    tplEditing.value = t
    tplForm.name = t.name
    tplForm.content = r.content
    showTplModal.value = true
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function saveTpl() {
  tplSaving.value = true
  try {
    if (tplEditing.value) {
      await api.put(`/api/templates/${encodeURIComponent(tplEditing.value.name)}`, { content: tplForm.content })
      toast('模板已保存', 'success')
    } else {
      await api.post('/api/templates', { name: tplForm.name.trim(), content: tplForm.content })
      toast('模板已创建', 'success')
    }
    showTplModal.value = false
    loadTemplates()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    tplSaving.value = false
  }
}

async function deleteTpl(t) {
  if (!(await ask('删除模板', `确定删除模板「${t.name}」？使用它的手动节点订阅将无法更新。`))) return
  tplDeleting.value = t.name
  try {
    await api.del(`/api/templates/${encodeURIComponent(t.name)}`)
    toast('已删除', 'success')
    loadTemplates()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    tplDeleting.value = ''
  }
}

const tplModalTitle = computed(() =>
  tplEditing.value ? (tplEditing.value.builtin ? '查看内置模板' : '编辑模板') : '新增模板')
const tplViewing = computed(() => !!tplEditing.value?.builtin)
</script>

<template>
  <div class="page">
    <n-card>
      <div class="head-row">
        <div>
          <h2 class="page-title">设置</h2>
          <p class="page-sub">插件全部配置 · 修改后点底部「保存」生效，涉及连接的改动会自动重启内核</p>
        </div>
        <n-tabs v-model:value="tab" type="segment" size="small" class="tabs">
          <n-tab-pane name="general"><template #tab>通用</template></n-tab-pane>
          <n-tab-pane name="subs"><template #tab>订阅</template></n-tab-pane>
          <n-tab-pane name="network"><template #tab>网络</template></n-tab-pane>
          <n-tab-pane name="core"><template #tab>内核</template></n-tab-pane>
          <n-tab-pane name="plugin"><template #tab>插件</template></n-tab-pane>
        </n-tabs>
      </div>
    </n-card>

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
            <n-input v-model:value="form.download_proxy" placeholder="https://gh-proxy.com" />
          </div>
        </div>
      </n-card>

      <!-- 界面显示：磨砂模糊（纯浏览器本地偏好，glass.js 持久化，不走后端设置/保存按钮） -->
      <n-card v-if="tab === 'general'" title="界面显示">
        <div class="rows">
          <div class="row">
            <div class="row-text">
              <span class="rt">菜单模式<HelpModal v-bind="HELP.menuMode" /></span>
            </div>
            <n-select
              v-model:value="menuMode"
              class="ctl"
              :options="[{ value: 'side', label: '左侧竖向菜单' }, { value: 'top', label: '顶部横向菜单' }]"
            />
          </div>
          <div class="row">
            <div class="row-text">
              <span class="rt">磨砂模糊<HelpModal v-bind="HELP.glass" /></span>
            </div>
            <n-switch v-model:value="glassEnabled" />
          </div>
          <div class="row" v-if="glassEnabled">
            <div class="row-text">
              <span class="rt">磨砂透明度<HelpModal v-bind="HELP.glassOpacity" /></span>
            </div>
            <div class="glass-ctl">
              <n-slider
                v-model:value="glassOpacity"
                :min="10" :max="100" :step="5" :format-tooltip="v => v + '%'"
              />
              <span class="glass-val mono">{{ glassOpacity }}%</span>
            </div>
          </div>
        </div>
      </n-card>
    </template>

    <!-- 订阅：配置模板（手动节点方式添加订阅时的骨架） -->
    <n-card v-if="tab === 'subs'">
      <template #header>
        <div class="card-head">
          <span>配置模板</span>
          <span class="card-sub">订阅页用「手动节点」添加时，节点注入模板的 proxies 段并替换 __ALL_PROXIES__；内置模板不可修改删除</span>
        </div>
      </template>
      <template #header-extra>
        <n-button size="small" type="primary" secondary @click="openTplNew">
          <template #icon><AppIcon name="plus" :size="13" /></template>新增
        </n-button>
      </template>
      <n-spin :show="tplLoading">
        <div v-if="!templates.length" class="tpl-empty">
          <n-empty description="还没有模板，点右上角「新增」创建" style="padding: 24px 0" />
        </div>
        <div v-else class="tpl-list">
          <div v-for="t in templates" :key="t.name" class="tpl-row">
            <div class="row-text">
              <span class="rt mono">
                {{ t.name }}
                <n-tag v-if="t.builtin" size="small" :bordered="false" type="default">内置</n-tag>
              </span>
              <span class="rs">{{ fmtTime(t.updated_at) }} · {{ fmtSize(t.size) }}</span>
            </div>
            <div class="btn-pair">
              <n-button size="small" @click="openTplEdit(t)">
                <template #icon><AppIcon :name="t.builtin ? 'file-text' : 'edit'" :size="13" /></template>
                {{ t.builtin ? '查看' : '编辑' }}
              </n-button>
              <n-button size="small" type="error" ghost :disabled="t.builtin" :loading="tplDeleting === t.name" @click="deleteTpl(t)">
                <template #icon><AppIcon name="trash" :size="13" /></template>删除
              </n-button>
            </div>
          </div>
        </div>
      </n-spin>
    </n-card>

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
          <n-select v-model:value="form.tun_stack" :options="[{ value: 'mixed', label: 'mixed' }, { value: 'system', label: 'system' }, { value: 'gvisor', label: 'gvisor' }]" class="ctl" />
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

    <!-- 断网自动重启：定时拨测检测地址，不通按上限自动重启内核（首页流量行有状态瓦片） -->
    <n-card v-if="tab === 'network'">
      <template #header>
        <div class="card-head">
          <span>断网自动重启</span>
          <span class="card-sub">定时访问检测地址，内核运行中但访问不通时自动重启内核尝试恢复；首页可查看检测状态</span>
        </div>
      </template>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">断网检测<HelpModal v-bind="HELP.autoReboot" /></span>
          </div>
          <n-switch v-model:value="form.auto_reboot_enabled" />
        </div>
        <div class="row" v-if="form.auto_reboot_enabled">
          <div class="row-text">
            <span class="rt">检测地址<HelpModal v-bind="HELP.autoRebootURL" /></span>
            <span v-if="!store.status?.running" class="rs">内核运行中才能配置与验证（检测经内核代理发出）</span>
          </div>
          <n-input-group>
            <n-input
              v-model:value="form.auto_reboot_url"
              placeholder="google.com"
              :disabled="!store.status?.running"
            />
            <n-button :loading="watchTesting" :disabled="!store.status?.running" @click="testWatchUrl">
              <template #icon><AppIcon name="zap" :size="13" /></template>检测
            </n-button>
          </n-input-group>
        </div>
        <div class="row" v-if="form.auto_reboot_enabled">
          <div class="row-text">
            <span class="rt">重启次数上限<HelpModal v-bind="HELP.autoRebootMax" /></span>
          </div>
          <n-input-number v-model:value="form.auto_reboot_max" :min="1" :max="10" class="num" />
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
          <div class="ver-side">
            <n-tag size="small" :bordered="false" :type="coreInfo.channel === 'alpha' ? 'warning' : 'default'">
              {{ coreInfo.channel === 'alpha' ? 'Alpha' : 'Release' }}
            </n-tag>
            <span class="mono" :style="{ color: coreInfo.installed ? 'var(--green)' : 'var(--red)' }">
              {{ coreInfo.installed ? coreInfo.version || '已安装' : '未安装' }}
            </span>
          </div>
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
            <span class="rt">内核渠道<HelpModal v-bind="HELP.coreChannel" /></span>
          </div>
          <n-select
            v-model:value="form.core_channel"
            class="ctl"
            :options="[
              { value: 'release', label: '正式版 Release' },
              { value: 'alpha', label: '抢先版 Alpha' },
            ]"
          />
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
            <n-button size="small" :disabled="coreUpgrading || coreUploading" @click="checkCore">
              <template #icon><AppIcon name="search" :size="13" /></template>检查
            </n-button>
            <n-button v-if="coreLatest?.has_update" type="primary" size="small" :loading="coreUpgrading" :disabled="coreUploading" @click="upgradeCore">
              <template #icon><AppIcon name="download" :size="13" /></template>{{ coreBtnText }}
            </n-button>
          </div>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">手动上传内核<HelpModal v-bind="HELP.coreUpload" /></span>
            <span class="rs">支持二进制或 .gz 压缩包，校验通过才安装为{{ form.core_channel === 'alpha' ? '抢先版（Alpha）' : '正式版（Release）' }}渠道内核</span>
          </div>
          <n-button size="small" :loading="coreUploading" :disabled="coreUpgrading" @click="pickCoreFile">
            <template #icon><AppIcon name="upload" :size="13" /></template>上传
          </n-button>
        </div>
        <div class="row" v-if="coreUpgrading">
          <div class="row-text">
            <span class="rt">升级进度</span>
            <span class="rs mono">{{ progText || '正在连接…' }}</span>
          </div>
          <div class="prog-side">
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
            <n-button size="small" quaternary type="error" @click="cancelUpgrade">取消</n-button>
          </div>
        </div>
      </div>
    </n-card>

    <!-- 手动上传内核的隐藏文件选择器（按钮在内核卡片内触发） -->
    <input ref="coreFileInput" type="file" class="core-file" @change="onCoreFilePicked" />

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
          <n-input v-model:value="form.plugin_repo" placeholder="owner/repo" />
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">GitHub 访问令牌<HelpModal v-bind="HELP.githubToken" /></span>
          </div>
          <n-input-group class="ctl">
            <n-input
              v-model:value="form.github_token"
              type="password"
              show-password-on="click"
              placeholder="ghp_… / github_pat_…，留空不启用"
            />
            <n-button title="清空令牌" :disabled="!form.github_token" @click="form.github_token = ''">
              <template #icon><AppIcon name="trash" :size="14" /></template>
            </n-button>
          </n-input-group>
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
              <template #icon><AppIcon name="search" :size="13" /></template>检查
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
          <div class="prog-side">
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
            <n-button size="small" quaternary type="error" @click="cancelUpgrade">取消</n-button>
          </div>
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
      :style="{ width: '580px', maxWidth: '94vw' }"
      @update:show="showBackupCreate = false"
    >
      <div class="backup-body">
        <div class="page-sub">备份内容：全部设置、订阅配置与列表、自定义规则、内核程序（不含运行缓存与日志）</div>
        <n-input v-model:value="backupName" placeholder="备份名称（可编辑）" @keyup.enter="createBackup" />
        <div class="backup-foot">
          <span class="page-sub">同名备份会被拒绝，换个名字即可</span>
          <n-button type="primary" size="small" :loading="backupCreating" @click="createBackup">备份</n-button>
        </div>
      </div>
    </n-modal>

    <!-- 备份列表弹窗：恢复 / 删除 -->
    <n-modal
      preset="card" title="备份列表" :show="showBackupList"
      :style="{ width: '720px', maxWidth: '94vw' }"
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

    <!-- 模板新增/编辑/查看弹窗：内置模板只读 -->
    <n-modal
      preset="card" :title="tplModalTitle" :show="showTplModal"
      :style="{ width: '780px', maxWidth: '94vw' }"
      @update:show="showTplModal = false"
    >
      <div class="tpl-form">
        <n-input
          v-model:value="tplForm.name"
          :disabled="!!tplEditing"
          :placeholder="tplViewing ? '' : '模板名（中文/字母/数字/中划线，如 我的模板）'"
        />
        <n-input
          v-model:value="tplForm.content"
          type="textarea"
          :rows="18"
          class="tpl-content"
          :readonly="tplViewing"
          placeholder="模板内容（YAML）…"
        />
        <p class="page-sub" v-if="tplViewing">内置模板不可修改或删除；需要改动可复制内容新建模板</p>
        <p class="page-sub" v-else>
          模板须包含 <code>proxies: ~</code> 占位段（添加订阅时被节点列表替换）；proxy-groups 里的 <code>__ALL_PROXIES__</code> 会展开成全部节点名
        </p>
        <div class="tpl-foot">
          <n-button quaternary @click="showTplModal = false">
            <template #icon><AppIcon name="close" :size="13" /></template>{{ tplViewing ? '关闭' : '取消' }}
          </n-button>
          <n-button v-if="!tplViewing" type="primary" :loading="tplSaving" @click="saveTpl">
            <template #icon><AppIcon name="check" :size="14" /></template>保存
          </n-button>
        </div>
      </div>
    </n-modal>

    <!-- 插件 tab 现在含在线仓库地址等可保存项，保存栏全 tab 显示；例外是订阅 tab——
         它只有即时操作的配置模板管理，没有可保存的表单项，显示保存按钮反而让人困惑 -->
    <div class="save-bar" v-if="tab !== 'subs'">
      <n-button type="primary" :loading="saving" :disabled="!loaded" @click="save">
        <template #icon><AppIcon name="save" :size="14" /></template>保存
      </n-button>
    </div>

    <!-- 升级确认弹窗：新版本徽标 + Release 更新日志（插件/内核升级共用） -->
    <UpdateModal
      v-model:show="updateAskShow"
      :title="updateAsk?.title"
      :latest="updateAsk?.latest"
      :current="updateAsk?.current"
      :notes="updateAsk?.notes"
      :pkg-text="updateAsk?.pkgText"
      @confirm="confirmUpdate"
    />
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
/* 行尾输入类控件统一列宽：下拉/输入框左缘对齐成一条竖线（右缘本就贴行尾）；
   按钮不参与拉宽，保持自然尺寸。变量挂在 .row 上，弹窗等区域的 btn-pair 不受影响 */
.row { --ctl-w: 210px; }
.row .ctl, .row > .n-input, .row .num { width: var(--ctl-w); }
.btn-pair { display: flex; gap: 8px; flex: none; }
.unit { color: var(--text-dim); font-size: 12.5px; }
/* 当前版本行右侧：渠道标识 + 版本/状态文本 */
.ver-side { display: flex; align-items: center; gap: 8px; flex: none; }
/* 令牌行：组整体等宽，输入框填充组内剩余（旁边图标按钮保持自然宽度） */
.row .n-input-group { width: var(--ctl-w); margin-left: auto; }
.row .n-input-group > .n-input { flex: 1; min-width: 0; }
/* 磨砂透明度：滑杆 + 当前值文本拼成一列等宽控件（与其他行控件右缘对齐） */
.glass-ctl { display: flex; align-items: center; gap: 10px; width: var(--ctl-w); }
.glass-ctl .n-slider { flex: 1; min-width: 0; }
.glass-val { flex: none; width: 36px; text-align: right; font-size: 12px; color: var(--text-dim); }
.prog { width: 180px; flex-shrink: 0; }
.prog-side { display: flex; align-items: center; gap: 8px; flex: none; }
/* 手动上传内核的隐藏文件选择器 */
.core-file { display: none; }
/* 常规流式布局：跟在卡片后面，不再悬浮遮挡内容 */
/* 保存栏常规右对齐（与卡片内容区自然对齐，不做额外缩进） */
.save-bar { display: flex; justify-content: flex-end; padding: 2px 0 10px; }
/* 备份弹窗：内容列 + 弹窗底部说明/按钮行；列表行左右分布 */
.backup-body { display: flex; flex-direction: column; gap: 12px; }
.backup-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.backup-list { display: flex; flex-direction: column; gap: 8px; max-height: 50vh; overflow-y: auto; }
.backup-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 12px; border-radius: 10px; background: var(--bg-card-2); border: 1px solid var(--border); }
/* 配置模板（订阅 tab）：列表行与弹窗 */
.tpl-list { display: flex; flex-direction: column; gap: 8px; }
.tpl-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 12px; border-radius: 10px; background: var(--bg-card-2); border: 1px solid var(--border); }
.tpl-row .rt { gap: 8px; font-size: 13px; }
.tpl-form { display: flex; flex-direction: column; gap: 12px; }
.tpl-content :deep(textarea) { font-family: var(--mono, ui-monospace, monospace); font-size: 12px; line-height: 1.55; }
.tpl-foot { display: flex; justify-content: flex-end; gap: 8px; }

/* ---- 手机/平板：行内控件换到文案下方铺满，开关保持靠右 ---- */
@media (max-width: 760px) {
  /* 顶部 segment tabs 窄屏放不下时横向滑动兜底（max-width:100% 只会裁剪不会滚）；
     同时收窄 tab 内边距，360px 手机 5 个 tab 刚好放得下、多数情况无需滑动 */
  .tabs { overflow-x: auto; }
  .tabs :deep(.n-tabs-tab) { padding: 6px 10px; }
  .row { flex-wrap: wrap; }
  .row .ctl, .row .num, .row > .n-input, .row .n-input-group { width: 100% !important; }
  .row > .n-switch { margin-left: auto; }
  .prog-side { width: 100%; }
  .prog { width: auto; flex: 1; }
  .tpl-row { flex-wrap: wrap; }
  .tpl-row .btn-pair { margin-left: auto; }
  /* 底部保存按钮填满横向宽度：窄屏下单按钮贴角显得小气 */
  .save-bar .n-button { width: 100%; }
}

/* 设置卡标题区与内容区分隔线：与首页各卡片的虚线同风格（n-card 内部结构需 :deep 穿透） */
:deep(.n-card-header) { border-bottom: 1px dashed var(--border); }
</style>
