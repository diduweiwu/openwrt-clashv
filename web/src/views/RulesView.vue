<script setup>
// 规则页：展示内核实际加载的路由规则（mihomo GET /rules），
// 可用来核对订阅里的规则是否真的生效（对照「连接页」的命中规则列）。
import { computed, onMounted, ref } from 'vue'
import { api } from '../api.js'
import { store, toast } from '../store.js'

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
      <button class="ghost sm" :disabled="loading || !store.status?.running" @click="load(false)">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <div class="search card">
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
      </svg>
      <input v-model="keyword" type="text" placeholder="搜索规则内容、类型或目标…" spellcheck="false">
      <button v-if="keyword" class="clear" @click="keyword = ''">✕</button>
    </div>

    <div v-if="!store.status?.running" class="card">
      <div class="empty-hint">内核未运行，启动后可查看规则</div>
    </div>
    <div v-else-if="filtered.length" class="card rule-card">
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
    </div>
    <div v-else class="card">
      <div class="empty-hint">
        {{ rules.length ? '没有匹配的规则' : '内核未加载任何规则——请在订阅配置中添加' }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.page-title { margin: 0 0 2px; font-size: 17px; }
.page-sub { margin: 0 0 12px; }
.search {
  display: flex; align-items: center; gap: 9px;
  padding: 10px 14px; margin-bottom: 12px;
  color: var(--text-dim);
}
.search input {
  flex: 1; border: none; outline: none; background: transparent;
  color: var(--text); font-size: 13.5px;
}
.search input::placeholder { color: var(--text-dim); }
.clear {
  border: none; background: var(--hover); color: var(--text-dim);
  width: 20px; height: 20px; border-radius: 50%;
  font-size: 11px; line-height: 1; flex: none;
}
.rule-card { padding: 0; overflow: hidden; }
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
.empty-hint { color: var(--text-dim); text-align: center; padding: 60px 0; }
</style>
