<script setup>
// 设置页：基础设置、TUN/DNS、内核更新（mihomo）、插件更新
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api } from '../api.js'
import { store, toast } from '../store.js'

// mihomo Release 提供的 linux 平台名（按指令集分组），value 与资产文件名一一对应
const CORE_PLATFORMS = [
  {
    group: 'x86',
    items: [
      ['linux-amd64-compatible', 'amd64-compatible（x86-64，兼容老 CPU）'],
      ['linux-amd64', 'amd64（x86-64）'],
      ['linux-amd64-v2', 'amd64-v2（需要 x86-64-v2 指令集）'],
      ['linux-amd64-v3', 'amd64-v3（需要 x86-64-v3 指令集）'],
      ['linux-386', '386（32 位 x86）'],
      ['linux-386-softfloat', '386-softfloat（32 位无浮点硬件）'],
    ],
  },
  {
    group: 'ARM',
    items: [
      ['linux-arm64', 'arm64（aarch64，多数新设备）'],
      ['linux-armv7', 'armv7（32 位带浮点）'],
      ['linux-armv6', 'armv6'],
      ['linux-armv5', 'armv5（老设备）'],
    ],
  },
  {
    group: 'MIPS',
    items: [
      ['linux-mips-softfloat', 'mips-softfloat（大端，常见 24K）'],
      ['linux-mips-hardfloat', 'mips-hardfloat（大端带浮点）'],
      ['linux-mipsle-softfloat', 'mipsle-softfloat（小端，常见 MT7621）'],
      ['linux-mipsle-hardfloat', 'mipsle-hardfloat（小端带浮点）'],
      ['linux-mips64', 'mips64（大端 64 位）'],
      ['linux-mips64le', 'mips64le（小端 64 位）'],
    ],
  },
  {
    group: '其他',
    items: [
      ['linux-riscv64', 'riscv64'],
      ['linux-loong64-abi1', 'loong64-abi1（旧世界）'],
      ['linux-loong64-abi2', 'loong64-abi2（新世界）'],
      ['linux-ppc64le', 'ppc64le'],
      ['linux-s390x', 's390x'],
    ],
  },
]

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
  auto_update: 12,
  token: '',
  core_arch: '',
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
      auto_update: s.auto_update,
      token: s.token || '',
      core_arch: s.core_arch || '',
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
    // 合并回完整设置（保留后端管理的字段）
    const cur = await api.get('/api/settings')
    const payload = { ...cur, ...form }
    const r = await api.put('/api/settings', payload)
    Object.assign(form, {
      mixed_port: r.settings.mixed_port,
      ui_port: r.settings.ui_port,
      tun: r.settings.tun,
      tun_stack: r.settings.tun_stack,
      dns: r.settings.dns,
      dns_mode: r.settings.dns_mode || 'fake-ip',
      dns_hijack: r.settings.dns_hijack || 'firewall',
      auto_update: r.settings.auto_update,
      token: r.settings.token || '',
      core_arch: r.settings.core_arch || '',
      download_proxy: r.settings.download_proxy || '',
    })
    if (r.error) toast('已保存，但内核重启失败：' + r.error, 'error')
    else if (r.restarted) toast('已保存，内核已重载生效', 'success')
    else toast('已保存' + (r.need_reload ? '（界面端口/令牌需重启服务后生效）' : ''), 'success')
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
  if (!confirm(`确认下载并安装 mihomo ${coreLatest.value.latest}？视网络情况可能需要几分钟`)) return
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
  if (!confirm(`确认下载并安装插件 ${pluginLatest.value.latest}？安装后需要重启服务`)) return
  pluginUpgrading.value = true
  startProgPoll()
  try {
    const r = await api.post('/api/plugin/upgrade')
    if (r.need_restart && store.status?.openwrt) {
      if (confirm('插件已下载，立即重启服务生效？')) {
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
  if (!confirm('确认重启 ClashV 服务？')) return
  try {
    await api.post('/api/service/restart')
    toast('服务重启中，请稍后刷新页面', 'success')
  } catch (e) {
    toast(e.message, 'error')
  }
}

onMounted(load)
onBeforeUnmount(stopProgPoll)
</script>

<template>
  <div class="page">
    <h1 class="page-title">设置</h1>

    <!-- 代理基础 -->
    <div class="card">
      <h3 class="sec">代理基础</h3>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">混合代理端口</span>
            <span class="rs">HTTP + SOCKS5 共用端口</span>
          </div>
          <input v-model.number="form.mixed_port" type="number" class="num">
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">订阅自动更新</span>
            <span class="rs">每 N 小时自动更新一次，0 为关闭</span>
          </div>
          <div class="with-unit">
            <input v-model.number="form.auto_update" type="number" class="num" style="width:76px">
            <span class="unit">小时</span>
          </div>
        </div>
      </div>
    </div>

    <!-- TUN 与 DNS -->
    <div class="card">
      <h3 class="sec">TUN 与 DNS</h3>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">TUN 模式</span>
            <span class="rs">接管全局流量（需内核 tun 模块）；关闭时自动用防火墙接管局域网 TCP（透明代理），无需手动配置</span>
          </div>
          <label class="switch">
            <input v-model="form.tun" type="checkbox">
            <span class="track"></span><span class="thumb"></span>
          </label>
        </div>
        <div class="row" v-if="form.tun">
          <div class="row-text">
            <span class="rt">TUN 协议栈</span>
            <span class="rs">mixed 兼顾性能与兼容性</span>
          </div>
          <select v-model="form.tun_stack" style="width:130px">
            <option value="mixed">mixed</option>
            <option value="system">system</option>
            <option value="gvisor">gvisor</option>
          </select>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">接管 DNS</span>
            <span class="rs">由 mihomo 处理局域网域名解析，透明代理/TUN 模式建议开启</span>
          </div>
          <label class="switch">
            <input v-model="form.dns" type="checkbox">
            <span class="track"></span><span class="thumb"></span>
          </label>
        </div>
        <div class="row" v-if="form.dns">
          <div class="row-text">
            <span class="rt">DNS 解析模式</span>
            <span class="rs">fake-ip 返回假 IP（198.18.x.x），域名规则匹配最准；redir-host 返回真实 IP，兼容不支持假 IP 的设备（国外域名已自动经代理用国外 DNS 防污染复核）</span>
          </div>
          <select v-model="form.dns_mode" style="width:160px">
            <option value="fake-ip">fake-ip（推荐）</option>
            <option value="redir-host">redir-host</option>
          </select>
        </div>
        <div class="row" v-if="form.dns && store.status?.openwrt">
          <div class="row-text">
            <span class="rt">DNS 劫持模式</span>
            <span class="rs">推荐防火墙转发：强制接管所有设备的 DNS（包括手动改过 DNS 的设备）。dnsmasq 转发只对使用路由器 DNS 的设备有效，设备自行配了 DNS 就会绕过内核（redir-host 下表现为部分网站打不开）</span>
          </div>
          <select v-model="form.dns_hijack" style="width:160px">
            <option value="firewall">防火墙转发（推荐）</option>
            <option value="dnsmasq">dnsmasq 转发</option>
            <option value="off">禁用</option>
          </select>
        </div>
      </div>
    </div>

    <!-- 内核 -->
    <div class="card">
      <h3 class="sec">内核（mihomo）</h3>
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
              内核资产的平台名，自动识别不对时手动选择<template v-if="coreInfo.platform">，当前识别为 <span class="mono">{{ coreInfo.platform }}</span></template>
            </span>
          </div>
          <select v-model="form.core_arch" style="width:250px">
            <option value="">自动识别{{ coreInfo.platform ? `（${coreInfo.platform}）` : '' }}</option>
            <optgroup v-for="g in CORE_PLATFORMS" :key="g.group" :label="g.group">
              <option v-for="p in g.items" :key="p[0]" :value="p[0]">{{ p[1] }}</option>
            </optgroup>
          </select>
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">下载加速前缀</span>
            <span class="rs">内核/插件从 GitHub 下载时套用此前缀（如 gh-proxy.com），留空直连</span>
          </div>
          <input v-model="form.download_proxy" placeholder="https://gh-proxy.com" style="width:210px">
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
            </span>
          </div>
          <div class="btn-pair">
            <button class="ghost sm" :disabled="coreUpgrading" @click="checkCore">检查更新</button>
            <button
              v-if="coreLatest?.has_update"
              class="primary sm"
              :disabled="coreUpgrading"
              @click="upgradeCore"
            >{{ coreBtnText }}</button>
          </div>
        </div>
        <div class="row" v-if="coreUpgrading">
          <div class="row-text">
            <span class="rt">升级进度</span>
            <span class="rs mono">{{ progText || '正在连接…' }}</span>
          </div>
          <div class="prog-bar" :class="{ indeterminate: !prog || prog.percent <= 0 }">
            <div class="prog-fill" :style="{ width: prog?.percent > 0 ? prog.percent + '%' : '100%' }"></div>
          </div>
        </div>
      </div>
    </div>

    <!-- 插件 -->
    <div class="card">
      <h3 class="sec">ClashV 插件</h3>
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
            </span>
          </div>
          <div class="btn-pair">
            <button class="ghost sm" :disabled="pluginUpgrading" @click="checkPlugin">检查更新</button>
            <button
              v-if="pluginLatest?.has_update"
              class="primary sm"
              :disabled="pluginUpgrading"
              @click="upgradePlugin"
            >{{ pluginBtnText }}</button>
          </div>
        </div>
        <div class="row" v-if="pluginUpgrading">
          <div class="row-text">
            <span class="rt">升级进度</span>
            <span class="rs mono">{{ progText || '正在连接…' }}</span>
          </div>
          <div class="prog-bar" :class="{ indeterminate: !prog || prog.percent <= 0 }">
            <div class="prog-fill" :style="{ width: prog?.percent > 0 ? prog.percent + '%' : '100%' }"></div>
          </div>
        </div>
        <div class="row" v-if="store.status?.openwrt">
          <div class="row-text">
            <span class="rt">重启服务</span>
            <span class="rs">修改界面端口或令牌后需重启</span>
          </div>
          <button class="ghost sm" @click="restartService">重启</button>
        </div>
      </div>
    </div>

    <!-- 访问控制 -->
    <div class="card">
      <h3 class="sec">访问控制</h3>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">界面访问令牌</span>
            <span class="rs">设置后局域网内打开界面需输入令牌，留空不启用</span>
          </div>
          <input v-model="form.token" placeholder="留空不启用" type="password" style="width:190px">
        </div>
        <div class="row">
          <div class="row-text">
            <span class="rt">界面端口</span>
            <span class="rs">修改后需重启服务生效</span>
          </div>
          <input v-model.number="form.ui_port" type="number" class="num">
        </div>
      </div>
    </div>

    <div class="save-bar">
      <button class="primary" :disabled="saving || !loaded" @click="save">
        {{ saving ? '保存中…' : '保存设置' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.sec { font-size: 15px; margin-bottom: 14px; }
.rows { display: flex; flex-direction: column; }
.row {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  padding: 11px 0;
  border-bottom: 1px solid var(--border);
}
.row:last-child { border-bottom: none; }
.row-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.rt { font-size: 13.5px; font-weight: 500; }
.rs { color: var(--text-dim); font-size: 12px; overflow: hidden; text-overflow: ellipsis; }
.num { width: 100px; }
.with-unit { display: flex; align-items: center; gap: 8px; }
.unit { color: var(--text-dim); font-size: 12.5px; }
.btn-pair { display: flex; gap: 8px; }
.prog-bar {
  width: 180px; height: 6px; border-radius: 3px;
  background: var(--border); overflow: hidden; flex-shrink: 0;
}
.prog-fill {
  height: 100%; border-radius: 3px;
  background: var(--green, #4caf7d);
  transition: width 0.4s ease;
}
.prog-bar.indeterminate .prog-fill {
  width: 100% !important;
  animation: prog-slide 1.1s ease-in-out infinite;
}
@keyframes prog-slide {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}
.save-bar { position: sticky; bottom: 0; display: flex; justify-content: flex-end; padding: 10px 0 2px; }
</style>
