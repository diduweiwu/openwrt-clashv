<script setup>
// 规则页：展示内核实际加载的路由规则（mihomo GET /rules），
// 可用来核对订阅里的规则是否真的生效（对照「连接页」的命中规则列）。
import { computed, onMounted, ref } from 'vue'
import { NButton, NCard, NEmpty, NInput } from 'naive-ui'
import { api } from '../api.js'
import { store, toast } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

const rules = ref([])
const loading = ref(false)
const keyword = ref('')

async function load(silent = true) {
  loading.value = !silent
  try {
    const r = await api.get('/api/rules')
    rules.value = r.rules || []
  } catch (e) {
    if (!silent) toast(e.message, 'error')
  } finally {
    loading.value = false
  }
}

// 过滤：内容 / 类型 / 目标 任一命中即可
const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  const out = []
  rules.value.forEach((r, i) => {
    if (
      k &&
      !(r.payload || '').toLowerCase().includes(k) &&
      !(r.type || '').toLowerCase().includes(k) &&
      !(r.proxy || '').toLowerCase().includes(k)
    ) return
    out.push({ ...r, idx: i + 1 })
  })
  return out
})

// Verge 风格配色：代理组橙色、DIRECT 绿、REJECT 红
function proxyClass(p) {
  if (p === 'DIRECT') return 'direct'
  if (p.startsWith('REJECT')) return 'reject'
  return 'group'
}

onMounted(() => load(false))
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h2 class="page-title">规则</h2>
        <p class="page-sub">
          内核当前加载的规则共 {{ rules.length }} 条<template v-if="keyword && filtered.length !== rules.length">，命中 {{ filtered.length }} 条</template>
          · 规则在订阅配置中定义，此处只读
        </p>
      </div>
      <n-button size="small" :loading="loading" :disabled="!store.status?.running" @click="load(false)"><template #icon><AppIcon name="refresh" :size="13" /></template>刷新</n-button>
    </div>

    <n-input v-model:value="keyword" placeholder="搜索规则内容、类型或目标…" clearable>
      <template #prefix>
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
        </svg>
      </template>
    </n-input>

    <n-card v-if="!store.status?.running" class="pad">
      <n-empty description="内核未运行，启动后可查看规则" />
    </n-card>
    <n-card v-else-if="filtered.length" class="rule-card">
      <div class="rule-scroll">
        <div v-for="r in filtered" :key="r.idx" class="rule-row">
          <span class="no mono">{{ r.idx }}</span>
          <div class="rule-main">
            <div class="payload mono" :title="r.payload">{{ r.payload || '—' }}</div>
            <div class="rtype">
              {{ r.type }}<template v-if="r.size > 0"> · {{ r.size }} 条子规则</template>
            </div>
          </div>
          <span class="proxy" :class="proxyClass(r.proxy || '')">{{ r.proxy || '—' }}</span>
        </div>
      </div>
    </n-card>
    <n-card v-else class="pad">
      <n-empty :description="rules.length ? '没有匹配的规则' : '内核未加载任何规则——请在订阅配置中添加'" />
    </n-card>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.page-title { margin: 0 0 2px; font-size: 17px; }
.pad :deep(.n-empty) { padding: 60px 0; }
.rule-card :deep(.n-card-content) { padding: 0; }
.rule-scroll { max-height: calc(100vh - 240px); min-height: 200px; overflow-y: auto; }
.rule-row {
  display: flex; align-items: center; gap: 14px;
  padding: 9px 16px;
  border-bottom: 1px solid var(--border);
}
.rule-row:last-child { border-bottom: none; }
.rule-row:hover { background: var(--hover); }
.no { width: 34px; flex: none; text-align: right; color: var(--text-dim); font-size: 11.5px; }
.rule-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.payload {
  font-size: 13px; font-weight: 500;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.rtype { font-size: 11.5px; color: var(--text-dim); }
.proxy {
  flex: none; max-width: 220px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: 12.5px; font-weight: 600; text-align: right;
}
.proxy.group { color: var(--orange); }
.proxy.direct { color: var(--green); }
.proxy.reject { color: var(--red); }
</style>
