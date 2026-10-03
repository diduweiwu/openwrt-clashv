<script setup>
// 订阅页：表格展示 + 过滤 + 单个/全部更新 + 激活 + 删除 + 定时更新配置（星期 + 时间点）
import { computed, h, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  NButton, NCard, NDataTable, NEmpty, NInput, NModal, NProgress, NTag,
  NSelect, NSwitch, NTimePicker,
} from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask, fmtBytes, fmtTime } from '../store.js'
import AppIcon from '../components/AppIcon.vue'
import SubFormModal from '../components/SubFormModal.vue'

const route = useRoute()

const profiles = ref([])
const active = ref('')
const showModal = ref(false)
const editing = ref(null) // 编辑中的订阅；null=添加模式
const busyId = ref('')
const updatingAll = ref(false)
const keyword = ref('')

function openAdd() {
  editing.value = null
  showModal.value = true
}
function openEdit(p) {
  editing.value = p
  showModal.value = true
}

async function load() {
  try {
    const data = await api.get('/api/profiles')
    profiles.value = data.profiles || []
    active.value = data.active || ''
    if (store.status && !store.status.profile && active.value) {
      const p = profiles.value.find(x => x.id === active.value)
      if (p) store.status.profile = p.name
    }
  } catch (e) {
    toast(e.message, 'error')
  }
}

// 过滤：名称 / 地址 / UA 任一命中即保留
const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  if (!k) return profiles.value
  return profiles.value.filter(p =>
    (p.name || '').toLowerCase().includes(k) ||
    (p.url || '').toLowerCase().includes(k) ||
    (p.ua || '').toLowerCase().includes(k))
})

async function update(p) {
  busyId.value = p.id
  try {
    const r = await api.post(`/api/profiles/${p.id}/update`)
    toast('订阅已更新' + (r.restarted ? '，内核已重载' : ''), 'success')
    await load()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    busyId.value = ''
  }
}

// 一键全部更新：后端批量拉取，激活订阅有变化时只重启一次内核
async function updateAll() {
  updatingAll.value = true
  try {
    const r = await api.post('/api/profiles/update_all')
    const okN = r.ok_count || 0
    const failN = r.fail_count || 0
    if (failN) {
      const bad = (r.results || []).find(x => !x.ok)
      toast(`更新完成：成功 ${okN} 个，失败 ${failN} 个（${bad?.name}：${bad?.error}）`, 'error', 6000)
    } else {
      toast(`已更新全部 ${okN} 个订阅` + (r.restarted ? '，内核已重载' : ''), 'success')
    }
    await load()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    updatingAll.value = false
  }
}

async function activate(p) {
  busyId.value = p.id
  try {
    const r = await api.post(`/api/profiles/${p.id}/activate`)
    if (r.error) toast('已激活，但内核重启失败：' + r.error, 'error')
    else toast(r.restarted ? '已切换并重载内核' : '已激活（内核未运行，下次启动生效）', 'success')
    await load()
    if (store.status) store.status = await api.get('/api/status')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    busyId.value = ''
  }
}

async function remove(p) {
  if (!(await ask('删除订阅', `确定删除订阅「${p.name}」？`))) return
  busyId.value = p.id
  try {
    await api.del('/api/profiles/' + p.id)
    toast('已删除', 'success')
    await load()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    busyId.value = ''
  }
}

// 订阅流量：已用/总量/到期（机场未提供则不显示）
function trafficOf(p) {
  if (!p.total) return null
  const used = (p.upload || 0) + (p.download || 0)
  return { used, total: p.total, percent: Math.min(100, (used / p.total) * 100) }
}
function fmtExpire(ts) {
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// ---- 表格列 ----
// 流量列：整个列表都没有流量信息时收窄到标题宽度（只显示横杠没必要占宽），有流量才展开
const hasTraffic = computed(() => profiles.value.some(p => trafficOf(p)))

const columns = computed(() => [
  {
    title: '名称', key: 'name', width: 190, ellipsis: { tooltip: true },
    render: p => h('span', { class: 'name-txt' }, p.name),
  },
  {
    title: '订阅地址', key: 'url', minWidth: 220, ellipsis: { tooltip: true },
    render: p => h('span', { class: 'mono url-txt' }, p.url),
  },
  {
    title: '流量', key: 'traffic', width: hasTraffic.value ? 180 : 64,
    render: p => {
      const t = trafficOf(p)
      if (!t) return h('span', { class: 'dim' }, '—')
      const txt = `${fmtBytes(t.used)} / ${fmtBytes(t.total)}（${Math.round(t.percent)}%）`
      return h('div', { class: 'traf' }, [
        h(NProgress, {
          class: 'bar', type: 'line', percentage: t.percent,
          showIndicator: false, height: 5, borderRadius: '3px',
        }),
        h('span', { class: 'mono dim t2 t-usage', title: txt }, txt),
        p.expire ? h('span', { class: 'dim t2' }, `到期 ${fmtExpire(p.expire)}`) : null,
      ])
    },
  },
  {
    title: '更新时间', key: 'updated_at', width: 170,
    render: p => h('div', { class: 'upd-cell' }, [
      h('span', null, fmtTime(p.updated_at)),
      h('span', { class: 'dim t2 ua' }, `${fmtBytes(p.size)}${p.ua ? ' · ' + p.ua : ''}`),
    ]),
  },
  {
    title: '操作', key: 'ops', width: 330,
    render: p => h('div', { class: 'ops-cell' }, [
      h(NButton, {
        size: 'small', loading: busyId.value === p.id,
        disabled: updatingAll.value, onClick: () => update(p),
      }, { icon: () => h(AppIcon, { name: 'refresh', size: 13 }), default: () => '更新' }),
      h(NButton, {
        size: 'small', type: 'error', ghost: true,
        disabled: busyId.value === p.id || updatingAll.value, onClick: () => remove(p),
      }, { icon: () => h(AppIcon, { name: 'trash', size: 13 }), default: () => '删除' }),
      h(NButton, {
        size: 'small',
        disabled: busyId.value === p.id || updatingAll.value, onClick: () => openEdit(p),
      }, { icon: () => h(AppIcon, { name: 'edit', size: 13 }), default: () => '编辑' }),
      p.id !== active.value
        ? h(NButton, {
            type: 'primary', size: 'small', disabled: busyId.value === p.id || updatingAll.value,
            onClick: () => activate(p),
          }, { icon: () => h(AppIcon, { name: 'upload', size: 13 }), default: () => '启用' })
        // 已启用的行在启用钮位置展示状态 tag，保持操作列等宽对齐
        : h(NTag, { size: 'small', round: true, bordered: false }, { default: () => '已启用' }),
    ]),
  },
])

// ---- 定时更新配置：星期多选 + 时间点 ----
const DAY_OPTIONS = [
  { label: '周一', value: 1 }, { label: '周二', value: 2 }, { label: '周三', value: 3 },
  { label: '周四', value: 4 }, { label: '周五', value: 5 }, { label: '周六', value: 6 },
  { label: '周日', value: 0 },
]

// 已保存的定时配置（驱动工具栏摘要）；弹窗编辑用 schedForm，保存成功才落回
const sched = reactive({ enabled: false, days: [], time: '04:00' })
const schedForm = reactive({ enabled: false, days: [], time: '04:00' })
const showSchedule = ref(false)
const schedSaving = ref(false)

function summaryOf(enabled, days, time) {
  if (!enabled) return '未启用'
  const labels = DAY_OPTIONS.filter(o => days.includes(o.value)).map(o => o.label)
  let when
  if (labels.length === 7) {
    when = '每天'
  } else if (labels.length > 2 && labels.every((l, i) => i === 0 || DAY_OPTIONS.findIndex(o => o.label === l) === DAY_OPTIONS.findIndex(o => o.label === labels[i - 1]) + 1)) {
    when = `${labels[0]}至${labels[labels.length - 1]}` // 连续区间压缩，如「周一至周五」
  } else {
    when = labels.join('、')
  }
  return `${when} ${time}`
}
const schedSummary = computed(() => summaryOf(sched.enabled, sched.days, sched.time))
const schedPreview = computed(() => summaryOf(schedForm.enabled, schedForm.days, schedForm.time))

async function fetchSchedule() {
  const r = await api.get('/api/profiles/schedule')
  sched.enabled = !!r.enabled
  sched.days = r.days || []
  sched.time = r.time || '04:00'
}

async function openSchedule() {
  try {
    await fetchSchedule()
  } catch (e) {
    toast(e.message, 'error')
    return
  }
  schedForm.enabled = sched.enabled
  schedForm.days = [...sched.days]
  schedForm.time = sched.time
  showSchedule.value = true
}

async function saveSchedule() {
  if (schedForm.enabled && !schedForm.days.length) {
    toast('请至少选择一个星期', 'warning')
    return
  }
  schedSaving.value = true
  try {
    const r = await api.put('/api/profiles/schedule', {
      enabled: schedForm.enabled,
      days: schedForm.days,
      time: schedForm.time,
    })
    sched.enabled = !!r.enabled
    sched.days = r.days || []
    sched.time = r.time || '04:00'
    toast(r.enabled ? `定时更新已开启：${schedSummary.value}` : '定时更新已关闭', 'success')
    showSchedule.value = false
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    schedSaving.value = false
  }
}

onMounted(() => {
  if (route.query.add) showModal.value = true
  load()
  fetchSchedule().catch(() => { /* 摘要拉取失败不打扰 */ })
})
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div class="title-line">
        <h1 class="page-title">订阅</h1>
        <span class="count-chip">
          {{ profiles.length }} 个订阅<template v-if="keyword"> · 命中 {{ filtered.length }}</template>
        </span>
      </div>
      <n-button type="primary" @click="openAdd"><template #icon><AppIcon name="plus" :size="14" /></template>添加</n-button>
    </div>

    <n-card v-if="profiles.length === 0" class="pad">
      <n-empty description="还没有订阅，点击右上角「添加」粘贴机场订阅链接" />
    </n-card>

    <n-card v-else>
      <!-- 工具栏：过滤 + 定时更新摘要/入口 + 一键全部更新 -->
      <div class="toolbar">
        <n-input v-model:value="keyword" size="small" placeholder="过滤订阅名称、地址或 UA…" clearable class="search">
          <template #prefix>
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
            </svg>
          </template>
        </n-input>
        <div class="tools">
          <n-button size="small" :title="`定时更新：${schedSummary}，点击配置`" @click="openSchedule">
            <template #icon><AppIcon name="clock" :size="13" /></template>
            定时 · {{ schedSummary }}
          </n-button>
          <n-button size="small" type="primary" ghost :loading="updatingAll" @click="updateAll">
            <template #icon><AppIcon name="refresh" :size="13" /></template>全部更新
          </n-button>
        </div>
      </div>

      <n-data-table
        size="small"
        :columns="columns"
        :data="filtered"
        :row-key="p => p.id"
        :row-class-name="p => (p.id === active ? 'row-active' : '')"
        :scroll-x="974"
      >
        <template #empty>
          <n-empty description="没有匹配的订阅" style="padding: 30px 0" />
        </template>
      </n-data-table>
    </n-card>

    <!-- 添加/编辑订阅弹窗（共用组件，editing 决定模式） -->
    <SubFormModal :open="showModal" :edit="editing" @close="showModal = false" @added="load" @saved="load" />

    <!-- 定时更新配置弹窗 -->
    <n-modal
      preset="card"
      title="定时更新订阅"
      :show="showSchedule"
      :style="{ width: '460px', maxWidth: '94vw' }"
      @update:show="showSchedule = false"
    >
      <div class="sched-form">
        <div class="s-switch">
          <div class="s-text">
            <span class="st">启用定时更新</span>
            <span class="ss">到点自动更新全部订阅；当前使用的订阅有变化时自动重载内核</span>
          </div>
          <n-switch v-model:value="schedForm.enabled" />
        </div>
        <template v-if="schedForm.enabled">
          <div class="s-row">
            <span class="f-label">更新星期（可多选）</span>
            <n-select v-model:value="schedForm.days" multiple :options="DAY_OPTIONS" placeholder="选择要更新的星期" :max-tag-count="5" />
          </div>
          <div class="s-row">
            <span class="f-label">更新时间</span>
            <n-time-picker v-model:formatted-value="schedForm.time" value-format="HH:mm" format="HH:mm" style="width: 150px" />
          </div>
        </template>
        <div class="s-foot">
          <span class="ss">保存后立即生效：{{ schedPreview }}</span>
          <div class="btn-pair">
            <n-button size="small" :disabled="schedSaving" @click="showSchedule = false">取消</n-button>
            <n-button type="primary" size="small" :loading="schedSaving" @click="saveSchedule">
              <template #icon><AppIcon name="check" :size="13" /></template>保存
            </n-button>
          </div>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
/* 统计数据与页面标题同行：基线对齐，窄屏放不下时换行 */
.title-line { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.count-chip { color: var(--text-dim); font-size: 12.5px; }
.pad :deep(.n-empty) { padding: 34px 0; }

.toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; flex-wrap: wrap; }
.toolbar .search { flex: 1; max-width: 380px; min-width: 170px; }
.tools { display: flex; gap: 8px; margin-left: auto; flex-wrap: wrap; }

/* 表格单元格内容（render 函数生成，无 scoped 属性，需 :deep 穿透） */
:deep(.name-txt) { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.url-txt) { color: var(--text-dim); font-size: 12px; }
:deep(.row-active > td) { background: var(--accent-soft) !important; }
:deep(.traf) { display: flex; flex-direction: column; gap: 3px; }
:deep(.t-usage) { font-size: 11px; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.upd-cell) { display: flex; flex-direction: column; gap: 2px; }
:deep(.t2) { font-size: 11.5px; }
:deep(.t2.ua) { max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.ops-cell) { display: flex; gap: 8px; }

/* 定时更新弹窗 */
.sched-form { display: flex; flex-direction: column; gap: 14px; }
.s-switch { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.s-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.st { font-size: 13.5px; font-weight: 500; }
.ss { color: var(--text-dim); font-size: 12px; }
.s-row { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12.5px; color: var(--text-dim); }
.s-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.btn-pair { display: flex; gap: 8px; }

/* ---- 手机/平板：工具栏换行、搜索独占一行；表格靠 scroll-x 横向滚动 ---- */
@media (max-width: 760px) {
  .toolbar .search { flex: 1 1 100%; max-width: 100%; }
  .tools { margin-left: 0; }
}
</style>
