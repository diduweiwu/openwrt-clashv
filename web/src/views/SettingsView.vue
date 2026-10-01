<script setup>
// 设置页：基础设置、TUN/DNS、内核更新（mihomo）、插件更新
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { NButton, NCard, NInput, NInputGroup, NInputNumber, NProgress, NSelect, NSpin, NSwitch, NTabs, NTabPane } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

function fmtMB(n) {
  return (n / 1048576).toFixed(1) + ' MB'
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
  auto_update: 12,
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
      auto_update: s.auto_update,
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
      auto_update: Number(form.auto_update) || 0,
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
      auto_update: r.settings.auto_update,
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

onMounted(load)
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
            <n-input-number v-model:value="form.mixed_port" :show-button="false" :min="1" :max="65535" class="num" />
          </div>
          <div class="row">
            <div class="row-text">
              <span class="rt">订阅自动更新</span>
              <span class="rs">到点自动拉取订阅更新并重载内核，0 为关闭<span class="rec">默认 12 小时，建议保持</span></span>
            </div>
            <n-input-number v-model:value="form.auto_update" :show-button="false" :min="0" :max="720" class="num">
              <template #suffix><span class="unit">小时</span></template>
            </n-input-number>
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

    <div class="save-bar" v-if="tab !== 'plugin'">
      <n-button type="primary" :loading="saving" :disabled="!loaded" @click="save">
        <template #icon><AppIcon name="save" :size="14" /></template>保存设置
      </n-button>
    </div>
  </div>
</template>

<style scoped>
.head-card :deep(.n-card-content) { padding: 10px 16px; }
.tabs { min-width: 0; }
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
/* 行描述尾部的「默认/建议」徽章：主题色软底小胶囊，辅助用户对照默认值做选择 */
.rec {
  display: inline-block;
  margin-left: 4px;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 11px;
  vertical-align: 1px;
}
.num { width: 110px; }
.unit { color: var(--text-dim); font-size: 12.5px; }
.btn-pair { display: flex; gap: 8px; flex: none; }
/* n-input-group 默认 flex 拉伸占满剩余宽度，收缩并靠右与行内其他控件一致 */
.row .n-input-group { width: fit-content; margin-left: auto; }
.prog { width: 180px; flex-shrink: 0; }
.save-bar { position: sticky; bottom: 0; display: flex; justify-content: flex-end; padding: 10px 0 2px; }
</style>
