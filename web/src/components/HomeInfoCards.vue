<script setup>
/**
 * 首页信息卡片组：IP 信息 / Clash 信息 / 系统信息 三张卡片（不带外层容器，
 * 与 UnlockPanel 一起摆进首页的两列网格，参考 Clash Verge 的信息卡布局）。
 *
 * 行样式统一「标签居左、值贴右」，行间虚线分隔；标题行是彩色图标块 + 标题。
 * Clash 信息行的齿轮复用首页原有的设置弹窗（经 emit 交回父组件打开）；
 * IP 数据来自共享模块 ipinfo.js（与 hero 徽标同源）。
 *
 * 用法示例（HomeView）：
 *   <HomeInfoCards
 *     :core-busy="coreUpgrading" :core-percent="corePercent" :core-stage="coreStageText"
 *     @check-core="checkCoreUpdate" @open-tun="openTun" @open-dns="openDns"
 *     @open-mode="showMode = true" @open-port="showPort = true" />
 */
import { computed, onMounted, ref } from 'vue'
import { NCard, NProgress, NTag } from 'naive-ui'
import { api } from '../api.js'
import { store, fmtUptime } from '../store.js'
import AppIcon from './AppIcon.vue'
import { ipData, ipLoading, loadIpInfo, ipText, ipPlace, copyIp } from '../ipinfo.js'
import { cardVisible } from '../homeLayout.js'

const props = defineProps({
  coreBusy: Boolean,  // 内核升级中（齿轮换进度环、不可点）
  corePercent: Number, // 升级下载百分比
  coreStage: String,   // 升级中的替代文案（下载中 xx% / 安装中…）
})
const emit = defineEmits(['check-core', 'open-tun', 'open-dns', 'open-mode', 'open-port'])

const status = computed(() => store.status)
const MODE_LABEL = { rule: '规则', global: '全局', direct: '直连' }

// 系统信息卡需要的设置项（开机自启/管理端口/LuCI 校验），只读一次
const sys = ref(null)
onMounted(async () => {
  try { sys.value = await api.get('/api/settings') } catch { /* 忽略，行显示 — */ }
})

const pluginText = computed(() =>
  status.value?.plugin_version === 'dev' ? '开发版' : 'v' + (status.value?.plugin_version || '…')
)
</script>

<template>
  <!-- IP 信息 -->
  <n-card v-if="cardVisible('ip')">
    <div class="ic-head">
      <span class="ic-chip chip-blue"><AppIcon name="globe" :size="16" /></span>
      <h3>IP 信息</h3>
      <button class="ic-refresh" :disabled="ipLoading" title="重新查询出口 IP" @click="loadIpInfo(status?.running, true)">
        <AppIcon name="refresh" :size="14" :class="{ spin: ipLoading }" />
      </button>
    </div>
    <div class="ic-rows">
      <div class="ic-row">
        <span class="ic-k">直连出口 IP</span>
        <button class="ic-ip mono" :title="ipData?.direct?.ok ? '点击复制' : ''" @click="copyIp(ipData?.direct)">{{ ipText(ipData?.direct) }}</button>
      </div>
      <div class="ic-row">
        <span class="ic-k">直连位置</span>
        <span class="ic-v dim" :title="ipPlace(ipData?.direct)">{{ ipPlace(ipData?.direct) }}</span>
      </div>
      <div class="ic-row">
        <span class="ic-k">代理出口 IP</span>
        <button class="ic-ip mono" :title="ipData?.proxy?.ok ? '点击复制' : ''" @click="copyIp(ipData?.proxy)">{{ ipText(ipData?.proxy) }}</button>
      </div>
      <div class="ic-row">
        <span class="ic-k">代理位置</span>
        <span class="ic-v dim" :title="ipPlace(ipData?.proxy)">{{ ipPlace(ipData?.proxy) }}</span>
      </div>
      <div v-if="ipData?.ts" class="ic-row">
        <span class="ic-k">查询源</span>
        <span class="ic-v dim">{{ ipData.direct?.source || '—' }} / {{ ipData.proxy?.source || '—' }}（缓存 60 秒）</span>
      </div>
    </div>
  </n-card>

  <!-- Clash 信息 -->
  <n-card v-if="cardVisible('clash')">
    <div class="ic-head">
      <span class="ic-chip chip-orange"><AppIcon name="zap" :size="16" /></span>
      <h3>Clash 信息</h3>
    </div>
    <div class="ic-rows">
      <div class="ic-row">
        <span class="ic-k">内核版本</span>
        <span class="ic-v">
          <!-- 检查更新齿轮沿用原瓦片的交互：升级中换迷你进度环并禁点 -->
          <button
            class="ic-gear"
            :class="{ busy: coreBusy && corePercent > 0 }"
            :title="coreBusy ? (coreStage || '内核升级中…') : '检查内核更新'"
            :disabled="coreBusy"
            @click="emit('check-core')"
          >
            <n-progress
              v-if="coreBusy && corePercent > 0"
              class="tag-prog" type="circle" color="currentColor"
              :percentage="corePercent" :show-indicator="false" :stroke-width="24"
            />
            <AppIcon v-else name="refresh" :size="12" />
          </button>
          <span class="mono" :class="{ dim: !!coreStage }" :title="coreStage || status?.core?.version || '未安装'">
            {{ coreStage || status?.core?.version || '未安装' }}
          </span>
        </span>
      </div>
      <div class="ic-row">
        <span class="ic-k">运行时长</span>
        <span class="ic-v mono">{{ status?.running ? fmtUptime(status.uptime) : '—' }}</span>
      </div>
      <div class="ic-row">
        <span class="ic-k">混合代理端口</span>
        <span class="ic-v">
          <span class="mono">{{ status?.mixed_port || '—' }}</span>
          <button class="ic-gear" title="混合端口使用说明" @click="emit('open-port')"><AppIcon name="gear" :size="12" /></button>
        </span>
      </div>
      <div class="ic-row">
        <span class="ic-k">出站模式</span>
        <span class="ic-v">
          <n-tag size="small" round :bordered="false" type="primary">{{ MODE_LABEL[status?.mode] || '规则' }}</n-tag>
          <button class="ic-gear" title="切换出站模式" @click="emit('open-mode')"><AppIcon name="gear" :size="12" /></button>
        </span>
      </div>
      <div class="ic-row">
        <span class="ic-k">TUN 模式</span>
        <span class="ic-v">
          <span class="mono" :class="{ dim: !status?.tun }">{{ status?.tun ? `开启 · ${status.tun_stack || 'mixed'}` : '未开启' }}</span>
          <button class="ic-gear" title="TUN 设置" @click="emit('open-tun')"><AppIcon name="gear" :size="12" /></button>
        </span>
      </div>
      <div class="ic-row">
        <span class="ic-k">DNS 接管</span>
        <span class="ic-v">
          <span class="mono" :class="{ dim: !status?.dns }">{{ status?.dns ? (status.dns_mode === 'fake-ip' ? 'Fake-IP' : status.dns_mode) : '未接管' }}</span>
          <button class="ic-gear" title="DNS 设置" @click="emit('open-dns')"><AppIcon name="gear" :size="12" /></button>
        </span>
      </div>
    </div>
  </n-card>

  <!-- 系统信息 -->
  <n-card v-if="cardVisible('sys')">
    <div class="ic-head">
      <span class="ic-chip chip-red"><AppIcon name="server" :size="16" /></span>
      <h3>系统信息</h3>
    </div>
    <div class="ic-rows">
      <div class="ic-row">
        <span class="ic-k">插件版本</span>
        <span class="ic-v mono">{{ pluginText }}</span>
      </div>
      <div class="ic-row">
        <span class="ic-k">运行环境</span>
        <span class="ic-v">{{ status?.openwrt ? 'OpenWrt（UCI 持久化）' : '标准系统（文件配置）' }}</span>
      </div>
      <div v-if="status?.openwrt" class="ic-row">
        <span class="ic-k">LuCI 登录校验</span>
        <n-tag size="small" round :bordered="false" :type="sys?.luci_auth ? 'success' : 'default'">{{ sys?.luci_auth ? '已开启' : '已关闭' }}</n-tag>
      </div>
      <div class="ic-row">
        <span class="ic-k">开机自启</span>
        <n-tag size="small" round :bordered="false" :type="sys?.enabled ? 'success' : 'default'">{{ sys?.enabled ? '已启用' : '未启用' }}</n-tag>
      </div>
      <div class="ic-row">
        <span class="ic-k">管理端口</span>
        <span class="ic-v mono">{{ sys?.ui_port || status?.ui_port || '—' }}</span>
      </div>
      <div class="ic-row">
        <span class="ic-k">当前订阅</span>
        <span class="ic-v" :class="{ dim: !status?.profile }">{{ status?.profile || '未设置' }}</span>
      </div>
    </div>
  </n-card>
</template>

<style scoped>
/* 标题行：彩色图标块 + 标题贴左、动作贴右（参考 Clash Verge 信息卡头） */
.ic-head { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; border-bottom: 1px dashed var(--border); padding-bottom: 10px; }
.ic-head h3 { font-size: 15px; }
.ic-chip {
  display: inline-flex; align-items: center; justify-content: center;
  width: 30px; height: 30px; border-radius: 9px; flex: none;
}
.chip-blue { background: var(--accent-soft); color: var(--accent); }
.chip-orange { background: rgba(232, 161, 60, 0.16); color: var(--orange, #e8a13c); }
.chip-red { background: rgba(224, 85, 133, 0.14); color: var(--red, #e05585); }
.ic-refresh {
  margin-left: auto;
  display: inline-flex; align-items: center; justify-content: center;
  width: 26px; height: 26px; padding: 0;
  border: none; border-radius: 7px; background: transparent; cursor: pointer;
  color: var(--text-dim); opacity: 0.7;
}
.ic-refresh:hover { opacity: 1; color: var(--accent); background: var(--hover); }
.ic-refresh:disabled { cursor: default; opacity: 0.7; color: var(--text-dim); }

/* 行：标签居左、值贴右，行间虚线分隔（与订阅/瓦片卡的 dashed 分隔同风格） */
.ic-rows { display: flex; flex-direction: column; }
.ic-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 9px 2px;
  border-top: 1px dashed var(--border);
  min-width: 0;
}
.ic-row:first-child { border-top: none; }
.ic-k { color: var(--text-dim); font-size: 13px; flex: none; }
.ic-v { font-size: 13px; font-weight: 600; display: inline-flex; align-items: center; gap: 6px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ic-v.dim, .dim { color: var(--text-dim); font-weight: 500; }
/* IP 值可点击复制 */
.ic-ip {
  font-size: 13px; font-weight: 600;
  background: transparent; border: none; cursor: pointer;
  color: var(--accent); padding: 2px 6px; border-radius: 6px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ic-ip:hover { background: var(--hover); }
/* 行尾小齿轮：与原瓦片 tile-gear 同款交互 */
.ic-gear {
  display: inline-flex; align-items: center; justify-content: center;
  width: 20px; height: 20px; padding: 0;
  border: none; border-radius: 6px; background: transparent; cursor: pointer;
  color: currentColor; opacity: 0.5;
}
.ic-gear:hover { opacity: 1; color: var(--accent); background: var(--hover); }
.ic-gear:disabled { cursor: default; opacity: 0.6; color: currentColor; background: transparent; }
.ic-gear.busy:disabled { opacity: 1; }
/* 迷你进度环：盖掉 naive circle 默认 120px（scoped 优先级不稳，带 .n-progress 拉高） */
.ic-gear .tag-prog.n-progress.n-progress--circle { width: 13px; height: 13px; display: block; }
.spin { animation: ic-spin 0.9s linear infinite; }
@keyframes ic-spin { to { transform: rotate(360deg); } }
</style>
