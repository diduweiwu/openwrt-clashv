<script setup>
// 规则页：自定义规则（置顶并入运行时配置）+ 内核实际加载的路由规则（mihomo GET /rules）。
// 自定义规则在弹窗里用「类型 / 值 / 目标」合成，保证 clash 规则格式；支持新增与编辑，
// 保存后内核自动重载。
import { computed, onMounted, ref, watch } from 'vue'
import { NButton, NCard, NCheckbox, NEmpty, NInput, NModal, NSelect } from 'naive-ui'
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

// ---- 弹窗：新增 / 编辑共用；editIndex = -1 表示新增，否则为待编辑行下标 ----
const showRuleModal = ref(false)
const editIndex = ref(-1)
// 记住上次用的类型/目标，连续录入少点两下
const lastUsed = { type: 'DOMAIN-SUFFIX', target: 'DIRECT' }

function openAdd() {
  editIndex.value = -1
  form.value = { type: lastUsed.type, value: '', target: lastUsed.target, noResolve: false }
  loadTargets() // 弹窗每次打开都刷新目标候选，跟随当前订阅的代理组
  showRuleModal.value = true
}

// 把合成好的规则串拆回表单字段（本页生成的规则格式固定，可逆）
function parseRule(rule) {
  const parts = rule.split(',').map(s => s.trim())
  if (parts[0] === 'MATCH') return { type: 'MATCH', value: '', target: parts[1] || 'DIRECT', noResolve: false }
  const noResolve = parts[parts.length - 1] === 'no-resolve'
  if (noResolve) parts.pop()
  const target = parts.length > 2 ? parts.pop() : 'DIRECT'
  const type = RULE_TYPES.some(t => t.value === parts[0]) ? parts[0] : 'DOMAIN-SUFFIX'
  return { type, value: parts.slice(1).join(','), target, noResolve }
}

function openEdit(i) {
  editIndex.value = i
  form.value = parseRule(custom.value[i])
  showRuleModal.value = true
}

async function saveRule() {
  const rule = composedRule.value
  if (!rule || saving.value) return
  if (custom.value.some((r, i) => r === rule && i !== editIndex.value)) {
    toast('该规则已存在', 'warning')
    return
  }
  const next = editIndex.value >= 0
    ? custom.value.map((r, i) => (i === editIndex.value ? rule : r))
    : [...custom.value, rule]
  if (await persist(next)) {
    lastUsed.type = form.value.type
    lastUsed.target = form.value.target
    showRuleModal.value = false
  }
}

async function removeRule(i) {
  if (saving.value) return
  await persist(custom.value.filter((_, idx) => idx !== i))
}

// 全量覆写保存；内核在运行时后端会自动重启加载。返回是否成功，供弹窗决定是否关闭
async function persist(next) {
  saving.value = true
  try {
    const r = await api.put('/api/rules/custom', { rules: next })
    custom.value = next
    if (r.error) toast('规则已保存，但内核重载失败：' + r.error, 'error', 5000)
    else toast(r.restarted ? '规则已保存，内核已重载生效' : '规则已保存，内核启动后生效', 'success')
    if (store.status?.running) load()
    return true
  } catch (e) {
    toast(e.message, 'error')
    return false
  } finally {
    saving.value = false
  }
}

// 自定义规则搜索：命中行携带原下标，编辑/删除仍定位到完整列表的原始位置
const customFiltered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  return custom.value
    .map((rule, idx) => ({ rule, idx }))
    .filter(x => !k || x.rule.toLowerCase().includes(k))
})

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

// 内核晚于挂载进入运行态时补拉代理组，目标下拉才能带出订阅里的分组
watch(
  () => store.status?.running,
  (running) => {
    if (running) loadTargets()
  },
)
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h2 class="page-title">规则</h2>
        <p class="page-sub">
          自定义规则置顶于订阅规则之前<template v-if="store.status?.running"> · 内核当前加载共 {{ rules.length }} 条</template><template v-if="keyword">，命中：自定义 {{ customFiltered.length }} / 运行时 {{ filtered.length }} 条</template>
        </p>
      </div>
      <n-button size="small" :loading="loading" :disabled="!store.status?.running" @click="load(false); loadTargets()">
        <template #icon><AppIcon name="refresh" :size="13" /></template>刷新
      </n-button>
    </div>

    <!-- 搜索框：同时过滤自定义规则与内核运行时规则 -->
    <n-input v-model:value="keyword" placeholder="搜索规则内容、类型或目标（自定义 + 运行时）…" clearable>
      <template #prefix>
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
        </svg>
      </template>
    </n-input>

    <!-- 自定义规则：弹窗内新增/编辑，内核运行时保存后自动重载 -->
    <n-card>
      <div class="sec-head">
        <h3><AppIcon class="sec-ico" name="layers" :size="15" />自定义规则</h3>
        <div class="sec-side">
          <span class="page-sub">共 {{ custom.length }} 条<template v-if="keyword"> · 命中 {{ customFiltered.length }} 条</template> · 越靠前越优先</span>
          <n-button size="small" type="primary" @click="openAdd">
            <template #icon><AppIcon name="plus" :size="13" /></template>添加规则
          </n-button>
        </div>
      </div>
      <n-empty v-if="!custom.length" description="还没有自定义规则，点右上角「添加规则」新建" style="padding: 26px 0" />
      <n-empty v-else-if="!customFiltered.length" description="没有匹配的自定义规则" style="padding: 26px 0" />
      <div v-else class="custom-list">
        <div v-for="x in customFiltered" :key="x.rule" class="rule-row custom">
          <span class="no mono">{{ x.idx + 1 }}</span>
          <div class="rule-main">
            <div class="payload mono">{{ x.rule }}</div>
            <div class="rtype">自定义 · 置顶</div>
          </div>
          <div class="row-ops">
            <n-button quaternary size="tiny" title="编辑该规则" :disabled="saving" @click="openEdit(x.idx)">
              <template #icon><AppIcon name="edit" :size="14" /></template>
            </n-button>
            <n-button quaternary size="tiny" title="删除该规则" :disabled="saving" @click="removeRule(x.idx)">
              <template #icon><AppIcon name="trash" :size="14" /></template>
            </n-button>
          </div>
        </div>
      </div>
    </n-card>

    <!-- 添加/编辑规则弹窗 -->
    <n-modal
      preset="card"
      :title="editIndex >= 0 ? '编辑规则' : '添加规则'"
      :show="showRuleModal"
      :style="{ width: '580px', maxWidth: '94vw' }"
      @update:show="showRuleModal = false"
    >
      <div class="rule-form">
        <div class="f-row">
          <span class="f-label">类型</span>
          <n-select v-model:value="form.type" :options="RULE_TYPES" :consistent-menu-width="false" />
        </div>
        <div class="f-row" v-if="form.type !== 'MATCH'">
          <span class="f-label">值</span>
          <n-input v-model:value="form.value" :placeholder="valuePlaceholder" />
        </div>
        <div class="f-row">
          <span class="f-label">目标</span>
          <n-select v-model:value="form.target" :options="targetOptions" placeholder="目标" />
        </div>
        <div class="f-row" v-if="noResolveVisible">
          <span class="f-label">选项</span>
          <n-checkbox v-model:checked="form.noResolve">no-resolve（命中后不做 DNS 解析，路由器上推荐）</n-checkbox>
        </div>
        <div class="f-preview mono">预览：{{ composedRule || '—' }}</div>
        <div class="f-foot">
          <span class="page-sub">保存后置顶生效，内核运行中自动重载</span>
          <div class="btn-pair">
            <n-button size="small" :disabled="saving" @click="showRuleModal = false">取消</n-button>
            <n-button type="primary" size="small" :loading="saving" :disabled="form.type !== 'MATCH' && !form.value.trim()" @click="saveRule">
              <template #icon><AppIcon name="check" :size="13" /></template>保存
            </n-button>
          </div>
        </div>
      </div>
    </n-modal>

    <n-card v-if="!store.status?.running" class="pad">
      <n-empty description="内核未运行，启动后可查看实际加载的规则" />
    </n-card>
    <n-card v-else-if="filtered.length" class="rule-card">
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
.sec-side { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }

/* 弹窗表单：标签在上、控件在下，底部实时预览合成结果 */
.rule-form { display: flex; flex-direction: column; gap: 14px; }
.f-row { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12.5px; color: var(--text-dim); }
.f-preview {
  padding: 9px 12px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12.5px;
  color: var(--accent);
  word-break: break-all;
}
.f-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.btn-pair { display: flex; gap: 8px; }

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
.row-ops { display: flex; gap: 2px; flex: none; }
/* 规则行全部展开不设内层滚动，长列表交给整页 body 滚动 */
.proxy {
  flex: none; max-width: 220px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: 12.5px; font-weight: 600; text-align: right;
}
.proxy.group { color: var(--orange); }
.proxy.direct { color: var(--green); }
.proxy.reject { color: var(--red); }

/* ---- 手机/平板 ---- */
@media (max-width: 760px) {
  .proxy { max-width: 40%; }
  .row-ops { flex-direction: column; }
}
</style>
