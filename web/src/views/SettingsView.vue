<script setup>
// 设置页：基础设置、TUN/DNS、内核更新（mihomo）、插件更新
import { onMounted, reactive, ref } from 'vue'
import { api } from '../api.js'
import { store, toast } from '../store.js'

const form = reactive({
  mixed_port: 7890,
  ui_port: 9097,
  allow_lan: false,
  tun: false,
  tun_stack: 'mixed',
  dns: true,
  auto_update: 12,
  token: '',
  core_arch: '',
  plugin_repo: '',
  download_proxy: 'https://gh-proxy.com',
})
const loaded = ref(false)
const saving = ref(false)

const coreInfo = ref({ installed: false, version: '', path: '', platform: '' })
const coreLatest = ref(null) // {current, latest, has_update}
const coreUpgrading = ref(false)
const pluginLatest = ref(null)
const pluginUpgrading = ref(false)

async function load() {
  try {
    const s = await api.get('/api/settings')
    Object.assign(form, {
      mixed_port: s.mixed_port,
      ui_port: s.ui_port,
      allow_lan: s.allow_lan,
      tun: s.tun,
      tun_stack: s.tun_stack,
      dns: s.dns,
      auto_update: s.auto_update,
      token: s.token || '',
      core_arch: s.core_arch || '',
      plugin_repo: s.plugin_repo || '',
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
      allow_lan: r.settings.allow_lan,
      tun: r.settings.tun,
      tun_stack: r.settings.tun_stack,
      dns: r.settings.dns,
      auto_update: r.settings.auto_update,
      token: r.settings.token || '',
      core_arch: r.settings.core_arch || '',
      plugin_repo: r.settings.plugin_repo || '',
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
  try {
    const r = await api.post('/api/core/upgrade')
    toast(`内核已更新到 ${r.version}`, 'success', 5000)
    coreInfo.value = await api.get('/api/core/status')
    coreLatest.value = null
    store.status = await api.get('/api/status')
  } catch (e) {
    toast(e.message, 'error', 6000)
  } finally {
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
    pluginUpgrading.value = false
  }
}

async function restartService() {
  if (!confirm('确认重启 openclash-air 服务？')) return
  try {
    await api.post('/api/service/restart')
    toast('服务重启中，请稍后刷新页面', 'success')
  } catch (e) {
    toast(e.message, 'error')
  }
}

onMounted(load)
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
            <span class="rt">允许局域网</span>
            <span class="rs">局域网设备可通过此路由器使用代理</span>
          </div>
          <label class="switch">
            <input v-model="form.allow_lan" type="checkbox">
            <span class="track"></span><span class="thumb"></span>
          </label>
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
            <span class="rs">接管路由器全局流量（无需配置 iptables）</span>
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
            <span class="rt">接管 DNS（fake-ip）</span>
            <span class="rs">TUN 模式建议开启，由 mihomo 处理域名解析</span>
          </div>
          <label class="switch">
            <input v-model="form.dns" type="checkbox">
            <span class="track"></span><span class="thumb"></span>
          </label>
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
            <span class="rs">与 OpenWrt 设备架构对应，识别不对时手动填写</span>
          </div>
          <div class="with-unit">
            <input v-model="form.core_arch" placeholder="自动检测" style="width:190px">
          </div>
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
            <button class="ghost sm" @click="checkCore">检查更新</button>
            <button
              v-if="coreLatest?.has_update"
              class="primary sm"
              :disabled="coreUpgrading"
              @click="upgradeCore"
            >{{ coreUpgrading ? '下载安装中…' : '升级' }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 插件 -->
    <div class="card">
      <h3 class="sec">openclash-air 插件</h3>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <span class="rt">当前版本</span>
            <span class="rs">更新源 GitHub 仓库</span>
          </div>
          <div class="with-unit">
            <input v-model="form.plugin_repo" placeholder="owner/repo" style="width:170px">
            <span class="unit mono">{{ store.status?.plugin_version }}</span>
          </div>
        </div>
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
            <button class="ghost sm" @click="checkPlugin">检查更新</button>
            <button
              v-if="pluginLatest?.has_update"
              class="primary sm"
              :disabled="pluginUpgrading"
              @click="upgradePlugin"
            >{{ pluginUpgrading ? '下载安装中…' : '升级' }}</button>
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
.save-bar { position: sticky; bottom: 0; display: flex; justify-content: flex-end; padding: 10px 0 2px; }
</style>
