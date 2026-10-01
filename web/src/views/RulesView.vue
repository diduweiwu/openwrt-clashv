<script setup>
// 规则页：自定义规则（置顶并入运行时配置）+ 内核实际加载的路由规则（mihomo GET /rules）。
// 自定义规则用「类型 / 值 / 目标」下拉合成，保证 clash 规则格式，保存后内核自动重载。
import { computed, onMounted, ref } from 'vue'
import { NButton, NCard, NCheckbox, NEmpty, NInput, NSelect } from 'naive-ui'
import { api } from '../api.js'
import { store, toast } from '../store.js'
import AppIcon from '../components/AppIcon.vue'

const rules = ref([]) // 内核运行时规则
const custom = ref([]) // 用户自定义规则（合成后的 clash 规则串）
const loading = ref(false)
const saving = ref(false)
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

async function loadCustom() {
  try {
    const r = await api.get('/api/rules/custom')
    custom.value = r.rules || []
  } catch { /* 静默：首次使用还没有该文件 */ }
}

// ---- 自定义规则表单：类型 / 值 / 目标 三段合成 clash 规则串 ----
const RULE_TYPES = [
  { label: 'DOMAIN（精确域名）', value: 'DOMAIN' },
  { label: 'DOMAIN-SUFFIX（域名后缀）', value: 'DOMAIN-SUFFIX' },
  { label: 'DOMAIN-KEYWORD（域名关键字）', value: 'DOMAIN-KEYWORD' },
  { label: 'DOMAIN-REGEX（域名正则）', value: 'DOMAIN-REGEX' },
  { label: 'GEOSITE（GeoSite 数据库）', value: 'GEOSITE' },
  { label: 'GEOIP（GeoIP 国家/地区码）', value: 'GEOIP' },
  { label: 'IP-CIDR（IPv4 网段）', value: 'IP-CIDR' },
  { label: 'IP-CIDR6（IPv6 网段）', value: 'IP-CIDR6' },
  { label: 'IP-ASN（ASN 编号）', value: 'IP-ASN' },
  { label: 'SRC-IP-CIDR（来源网段）', value: 'SRC-IP-CIDR' },
  { label: 'SRC-PORT（来源端口）', value: 'SRC-PORT' },
  { label: 'DST-PORT（目标端口）', value: 'DST-PORT' },
  { label: 'PROCESS-NAME（进程名）', value: 'PROCESS-NAME' },
  { label: 'MATCH（兜底，放行其余全部）', value: 'MATCH' },
]
// IP 类规则可加 no-resolve：命中时不再做 DNS 解析（路由器上推荐勾选）
const NORESOLVE_TYPES = ['GEOIP', 'IP-CIDR', 'IP-CIDR6', 'IP-ASN']

// 值输入框按类型给示例，降低格式门槛
const VALUE_HINT = {
  DOMAIN: 'www.example.com',
  'DOMAIN-SUFFIX': 'example.com',
  'DOMAIN-KEYWORD': 'google',
  'DOMAIN-REGEX': '^.*\\.example\\.com$',
  GEOSITE: 'youtube',
  GEOIP: 'CN',
  'IP-CIDR': '192.168.1.0/24',
  'IP-CIDR6': '2620:0:2d0::/48',
  'IP-ASN': '13335',
  'SRC-IP-CIDR': '192.168.1.0/24',
  'SRC-PORT': '7777',
  'DST-PORT': '443',
  'PROCESS-NAME': 'curl',
}

const form = ref({ type: 'DOMAIN-SUFFIX', value: '', target: 'DIRECT', noResolve: false })
const noResolveVisible = computed(() => NORESOLVE_TYPES.includes(form.value.type))
const valuePlaceholder = computed(() =>
  form.value.type === 'MATCH' ? '兜底规则无需填写值' : `例：${VALUE_HINT[form.value.type] || ''}`,
)

// 目标候选：内核运行时带出全部代理组，供流量导向指定分组
const FIXED_TARGETS = ['DIRECT', 'REJECT']
const targetNames = ref([])
const targetOptions = computed(() => {
  const opts = targetNames.value.map(n => ({ label: n, value: n }))
  for (const t of FIXED_TARGETS) {
    if (!targetNames.value.includes(t)) opts.push({ label: t, value: t })
  }
  return opts
})

async function loadTargets() {
  if (!store.status?.running) { targetNames.value = []; return }
  try {
    const data = await api.get('/api/proxies')
    targetNames.value = Object.values(data.proxies || {})
      .filter(p => ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay'].includes(p.type) && p.name !== 'GLOBAL')
      .map(p => p.name)
    if (form.value.target !== 'DIRECT' && form.value.target !== 'REJECT' &&
        !targetNames.value.includes(form.value.target)) {
      form.value.target = 'DIRECT' // 上次选的组随订阅消失时回退直连
    }
  } catch { /* 内核未运行时只有 DIRECT/REJECT 可选 */ }
}

// 三段合成；MATCH 只有目标一段，IP 类规则按勾选拼 no-resolve
const composedRule = computed(() => {
  const f = form.value
  if (f.type === 'MATCH') return `MATCH,${f.target}`
  const v = f.value.trim()
  if (!v) return ''
  return `${f.type},${v},${f.target}${f.noResolve ? ',no-resolve' : ''}`
})

async function addRule() {
  const rule = composedRule.value
  if (!rule || saving.value) return
  if (custom.value.includes(rule)) {
    toast('该规则已存在', 'warning')
    return
  }
  await persist([...custom.value, rule])
  // 保留类型与目标，清掉值方便连续录入
  form.value.value = ''
  form.value.noResolve = false
}

async function removeRule(i) {
  if (saving.value) return
  await persist(custom.value.filter((_, idx) => idx !== i))
}

// 全量覆写保存；内核在运行时后端会自动重启加载
async function persist(next) {
  saving.value = true
  try {
    const r = await api.put('/api/rules/custom', { rules: next })
    custom.value = next
    if (r.error) toast('规则已保存，但内核重载失败：' + r.error, 'error', 5000)
    else toast(r.restarted ? '规则已保存，内核已重载生效' : '规则已保存，内核启动后生效', 'success')
    if (store.status?.running) load()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    saving.value = false
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

onMounted(() => {
  loadCustom()
  loadTargets()
  load(true) // 挂载时静默加载：内核未运行是常态，不打扰
})
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h2 class="page-title">规则</h2>
        <p class="page-sub">
          自定义规则置顶于订阅规则之前<template v-if="store.status?.running"> · 内核当前加载共 {{ rules.length }} 条</template><template v-if="keyword && filtered.length !== rules.length">，命中 {{ filtered.length }} 条</template>
        </p>
      </div>
      <n-button size="small" :loading="loading" :disabled="!store.status?.running" @click="load(false); loadTargets()">
        <template #icon><AppIcon name="refresh" :size="13" /></template>刷新
      </n-button>
    </div>

    <!-- 自定义规则：新增即保存，内核运行时自动重载 -->
    <n-card>
      <div class="sec-head">
        <h3><AppIcon class="sec-ico" name="layers" :size="15" />自定义规则</h3>
        <span class="page-sub">按类型 / 值 / 目标合成 clash 规则，越靠前越优先命中</span>
      </div>
      <div class="add-row">
        <n-select
          v-model:value="form.type"
          class="sel-type"
          :options="RULE_TYPES"
          :consistent-menu-width="false"
        />
        <n-input
          v-model:value="form.value"
          class="in-value"
          :disabled="form.type === 'MATCH'"
          :placeholder="valuePlaceholder"
        />
        <span class="arrow">→</span>
        <n-select v-model:value="form.target" class="sel-target" :options="targetOptions" placeholder="目标" />
        <n-checkbox v-if="noResolveVisible" v-model:checked="form.noResolve">no-resolve</n-checkbox>
        <n-button type="primary" :loading="saving" :disabled="form.type !== 'MATCH' && !form.value.trim()" @click="addRule">
          <template #icon><AppIcon name="plus" :size="13" /></template>添加
        </n-button>
      </div>
      <n-empty v-if="!custom.length" description="还没有自定义规则，用上方表单添加" style="padding: 26px 0" />
      <div v-else class="custom-list">
        <div v-for="(c, i) in custom" :key="c" class="rule-row custom">
          <span class="no mono">{{ i + 1 }}</span>
          <div class="rule-main">
            <div class="payload mono">{{ c }}</div>
            <div class="rtype">自定义 · 置顶</div>
          </div>
          <n-button quaternary size="tiny" title="删除该规则" :disabled="saving" @click="removeRule(i)">
            <template #icon><AppIcon name="trash" :size="14" /></template>
          </n-button>
        </div>
      </div>
    </n-card>

    <n-input v-model:value="keyword" placeholder="搜索运行时规则内容、类型或目标…" clearable>
      <template #prefix>
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
        </svg>
      </template>
    </n-input>

    <n-card v-if="!store.status?.running" class="pad">
      <n-empty description="内核未运行，启动后可查看实际加载的规则" />
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
      <n-empty :description="rules.length ? '没有匹配的规则' : '内核未加载任何规则——请在订阅或上方自定义规则中添加'" />
    </n-card>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.page-title { margin: 0 0 2px; font-size: 17px; }
.pad :deep(.n-empty) { padding: 60px 0; }

.sec-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 12px; flex-wrap: wrap; }
.sec-head h3 { font-size: 15px; }
.sec-ico { color: var(--accent); margin-right: 7px; vertical-align: -2px; }

/* 新增表单：类型 / 值 / 目标 一行合成 */
.add-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.sel-type { width: 240px; flex: none; }
.in-value { flex: 1; min-width: 160px; }
.arrow { color: var(--text-dim); flex: none; }
.sel-target { width: 200px; flex: none; }

.custom-list { display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: 10px; overflow: hidden; }
.rule-row {
  display: flex; align-items: center; gap: 14px;
  padding: 9px 16px;
  border-bottom: 1px solid var(--border);
}
.rule-card :deep(.n-card-content) { padding: 0; }
.rule-row:last-child { border-bottom: none; }
.rule-row:hover { background: var(--hover); }
.custom-list .rule-row { padding: 9px 12px; }
.no { width: 34px; flex: none; text-align: right; color: var(--text-dim); font-size: 11.5px; }
.rule-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.payload {
  font-size: 13px; font-weight: 500;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.rtype { font-size: 11.5px; color: var(--text-dim); }
.rule-scroll { max-height: calc(100vh - 240px); min-height: 200px; overflow-y: auto; }
.proxy {
  flex: none; max-width: 220px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: 12.5px; font-weight: 600; text-align: right;
}
.proxy.group { color: var(--orange); }
.proxy.direct { color: var(--green); }
.proxy.reject { color: var(--red); }

/* ---- 手机/平板：新增表单竖排堆叠，箭头隐藏 ---- */
@media (max-width: 760px) {
  .add-row { flex-direction: column; align-items: stretch; }
  .sel-type, .sel-target { width: 100%; flex: none; }
  .arrow { display: none; }
  .add-row :deep(.n-checkbox) { justify-content: flex-start; }
  .proxy { max-width: 40%; }
}
</style>
