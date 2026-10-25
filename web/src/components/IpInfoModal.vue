<script setup>
/**
 * 出口 IP 详情弹窗：国内直连出口与代理出口的 IP 信息，含多个查询源
 * （ip.sb / ip-api.com）逐源展示与手动刷新，思路参考 ip.skk.moe 的本机 IP 查询。
 *
 * 数据来自 GET /api/ipinfo（后端 60s 缓存，refresh=1 强制重查）。
 *
 * 用法示例：
 *   <IpInfoModal v-model:show="ipModalShow" />
 */
import { computed, ref, watch } from 'vue'
import { NButton, NModal, NTag } from 'naive-ui'
import { api } from '../api.js'
import { toast, fmtTime } from '../store.js'
import AppIcon from './AppIcon.vue'

const props = defineProps({
  show: Boolean, // 弹窗显隐（v-model:show）
})
const emit = defineEmits(['update:show'])

const loading = ref(false)
const info = ref(null) // {direct, proxy, ts}

// 首次打开时拉一次数据（已缓存则秒回）
watch(() => props.show, (v) => {
  if (v && !info.value) load(false)
})

async function load(refresh) {
  loading.value = true
  try {
    info.value = await api.get('/api/ipinfo' + (refresh ? '?refresh=1' : ''))
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    loading.value = false
  }
}

// 单个出口的展示行：IP + 位置 + 运营商 + 命中源
const rows = computed(() => {
  const d = info.value
  if (!d) return []
  return [
    { key: 'direct', label: '国内直连出口', sub: '不走代理的真实出口（路由器本机直连）', e: d.direct },
    { key: 'proxy', label: '代理出口', sub: '经内核代理端口后的出口（节点 IP）', e: d.proxy },
  ]
})

function copyIP(ip) {
  const done = () => toast('已复制 ' + ip, 'success')
  navigator.clipboard?.writeText(ip).then(done).catch(() => {
    // 非 secure 上下文（LuCI http 内嵌）clipboard 不可用，退 execCommand
    const ta = document.createElement('textarea')
    ta.value = ip
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
      <div v-for="row in rows" :key="row.key" class="ip-sec">
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
          <div class="ip-line ip-meta">{{ [row.e.country, row.e.city, row.e.isp].filter(Boolean).join(' · ') || '—' }}</div>
        </template>
        <div v-else class="ip-line ip-meta dim">{{ row.e?.error || '未查询' }}</div>
        <div class="ip-sub">{{ row.sub }}</div>
      </div>
      <div class="ip-foot">
        <span class="page-sub">{{ info?.ts ? '采样于 ' + fmtTime(info.ts) : '' }}（结果缓存 60 秒）</span>
        <n-button size="small" :loading="loading" @click="load(true)">
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
.ip-meta { font-size: 12.5px; }
.ip-meta.dim { color: var(--text-dim); }
.ip-sub { color: var(--text-dim); font-size: 11.5px; }
.ip-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 2px; }
</style>
