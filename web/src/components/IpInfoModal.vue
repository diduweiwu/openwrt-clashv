<script setup>
/**
 * 出口 IP 详情弹窗：国内直连出口与代理出口的完整地理信息（地址/ISP/所属组织/
 * ASN/大洲/时区/坐标/位置国旗），附本机 IPv6 出网探测（支持性 + 地址），
 * 思路参考 ip.skk.moe 的本机 IP 查询。
 *
 * 数据与首页瓦片共用 ipinfo.js 同一份状态（browser 模式页面内直查 /
 * openwrt 模式走 GET /api/ipinfo 后端查询，两种模式的返回结构一致，
 * 后端在 netcheck.go 补齐同样的扩展字段），底部显示当前检测位置。
 *
 * 用法示例：
 *   <IpInfoModal v-model:show="ipModalShow" />
 */
import { computed, watch } from 'vue'
import { NButton, NModal, NTag } from 'naive-ui'
import { toast, fmtTime, store } from '../store.js'
import AppIcon from './AppIcon.vue'
import { ipData, ipLoading, loadIpInfo, probeMode } from '../ipinfo.js'

const props = defineProps({
  show: Boolean, // 弹窗显隐（v-model:show）
})
const emit = defineEmits(['update:show'])

// 首次打开时若无数据则拉一次（与瓦片同源，已有缓存则秒回）
watch(() => props.show, (v) => {
  if (v && !ipData.value) loadIpInfo(store.status?.running, false)
})

// 重新查询：绕过 60 秒缓存
function refresh() {
  loadIpInfo(store.status?.running, true)
}

// 直连/代理两个出口分节
const sections = computed(() => [
  { key: 'direct', label: '国内直连出口', sub: '不走代理的真实出口', e: ipData.value?.direct },
  { key: 'proxy', label: '代理出口', sub: '经内核代理端口后的出口（节点 IP）', e: ipData.value?.proxy },
])

// IPv6 出网探测结果（直连侧：请求纯 IPv6 端点，可达即支持）
const v6 = computed(() => ipData.value?.ipv6)

// 检测位置文案（browser / openwrt 模式语义不同，弹窗底部说明数据从哪查的）
const probeText = computed(() =>
  probeMode.value === 'openwrt'
    ? '检测位置：OpenWrt 后端（直连走路由器本机网络，代理经混合端口）'
    : '检测位置：当前浏览器（直连/代理按分流规则经浏览器实际链路）'
)

/**
 * 出口详情展示行：[标签, 值] 列表，空值行直接不渲染。
 * @param {object} e ipEntry（ok/ip/isp/org/asn/asnOrg/continent/timezone/lat/lon/city/country/countryCode）
 * @returns {Array<[string, string]>}
 */
function detailRows(e) {
  if (!e?.ok) return []
  const rows = [
    ['ISP', e.isp],
    ['所属组织', e.org],
    ['ASN', e.asn],
    ['ASN 组织', e.asnOrg],
    ['大洲', e.continent],
    ['时区', e.timezone],
    ['坐标', e.lat || e.lon ? `${e.lat}, ${e.lon}` : ''],
    ['位置', locText(e)],
  ]
  return rows.filter(([, v]) => v)
}

/**
 * 位置文案：国旗 emoji（国家代码转 Unicode 区域指示符）+「城市, 国家」。
 * @param {object} e ipEntry
 * @returns {string} 如「🇨🇳 成都, 中国」；无信息时返回空串（整行隐藏）
 */
function locText(e) {
  const place = [e.city, e.country].filter(Boolean).join(', ')
  if (!place) return ''
  const flag = e.countryCode && e.countryCode.length === 2
    ? String.fromCodePoint(...[...e.countryCode.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
    : ''
  return (flag ? flag + ' ' : '') + place
}

function copyIP(ip) {
  const done = () => toast('已复制 ' + ip, 'success')
  navigator.clipboard?.writeText(ip).then(done).catch(() => {
    // 非 secure 上下文（LuCI http 内嵌）clipboard 不可用，退 execCommand
    const ta = document.createElement('textarea')
    ta.value = ip
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
    done()
  })
}
</script>

<template>
  <n-modal
    preset="card"
    title="出口 IP 详情"
    :show="show"
    :style="{ width: '560px', maxWidth: '94vw' }"
    @update:show="(v) => emit('update:show', v)"
  >
    <div class="ip-body">
      <div v-for="row in sections" :key="row.key" class="ip-sec">
        <div class="ip-sec-head">
          <span class="ip-label">{{ row.label }}</span>
          <n-tag size="small" round :bordered="false" :type="row.e?.ok ? 'success' : 'default'">
            {{ row.e?.ok ? row.e.source : '查询失败' }}
          </n-tag>
        </div>
        <template v-if="row.e?.ok">
          <button class="ip-line mono ip-main" :title="'点击复制 ' + row.e.ip" @click="copyIP(row.e.ip)">
            {{ row.e.ip }}
            <AppIcon name="copy" :size="12" />
          </button>
          <!-- 详情行：标签居左、值贴右，虚线分隔（与订阅/信息卡 srow 同风格） -->
          <div v-for="([k, v]) in detailRows(row.e)" :key="k" class="ip-row-line">
            <span class="ip-k">{{ k }}</span>
            <span class="ip-v" :class="{ mono: k === '坐标' }">{{ v }}</span>
          </div>
        </template>
        <div v-else class="ip-line ip-meta dim">{{ row.e?.error || '未查询' }}</div>
        <div class="ip-sub">{{ row.sub }}</div>
      </div>

      <!-- IPv6 连接：本机 IPv6 出网探测（直连侧），支持时展示探测到的地址 -->
      <div class="ip-sec">
        <div class="ip-sec-head">
          <span class="ip-label">IPv6 连接</span>
          <n-tag v-if="v6?.ok" size="small" round :bordered="false" type="success">
            <AppIcon name="check" :size="11" />&nbsp;支持
          </n-tag>
          <n-tag v-else size="small" round :bordered="false" type="error">
            <AppIcon name="close" :size="11" />&nbsp;不支持
          </n-tag>
        </div>
        <template v-if="v6?.ok">
          <button class="ip-line mono ip-main" :title="'点击复制 ' + v6.ip" @click="copyIP(v6.ip)">
            {{ v6.ip }}
            <AppIcon name="copy" :size="12" />
          </button>
          <div class="ip-line ip-meta dim">本机支持 IPv6 出网（来源 {{ v6.source }}）</div>
        </template>
        <div v-else class="ip-line ip-meta dim">{{ v6?.error || '当前网络不支持 IPv6 连接。' }}</div>
      </div>

      <div class="ip-foot">
        <span class="page-sub">
          {{ ipData?.ts ? '采样于 ' + fmtTime(ipData.ts) + '（缓存 60 秒）· ' : '' }}{{ probeText }}
        </span>
        <n-button size="small" :loading="ipLoading" @click="refresh">
          <template #icon><AppIcon name="refresh" :size="13" /></template>重新查询
        </n-button>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.ip-body { display: flex; flex-direction: column; gap: 10px; }
.ip-sec {
  display: flex; flex-direction: column; gap: 5px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 11px 13px;
}
.ip-sec-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.ip-label { font-weight: 600; font-size: 13px; }
.ip-main {
  display: inline-flex; align-items: center; gap: 7px;
  align-self: flex-start;
  font-size: 17px; font-weight: 700; color: var(--accent);
  background: var(--accent-soft); border-radius: 8px;
  padding: 4px 12px; border: none; cursor: pointer; font-family: inherit;
}
.ip-main:hover { filter: brightness(1.06); }
/* 详情行：标签居左、值贴右，虚线分隔（与订阅/信息卡 srow 同风格） */
.ip-row-line {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 6px 2px;
  border-top: 1px dashed var(--border);
  min-width: 0;
}
.ip-row-line:first-of-type { border-top: none; }
.ip-k { color: var(--text-dim); font-size: 12.5px; flex: none; }
.ip-v { font-size: 12.5px; font-weight: 500; text-align: right; min-width: 0; overflow-wrap: anywhere; }
.ip-meta { font-size: 12.5px; }
.ip-meta.dim { color: var(--text-dim); }
.ip-sub { color: var(--text-dim); font-size: 11.5px; }
.ip-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 2px; }
.ip-foot .page-sub { text-align: left; }
</style>
