<script setup>
// 订阅页：列表展示 + 过滤 + 单个/全部更新 + 激活 + 删除 + 定时更新配置（星期 + 时间点）
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  NButton, NCard, NEmpty, NInput, NList, NListItem, NModal, NProgress, NTag,
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

// 当前使用中的订阅名（标题下描述行）
const activeName = computed(() => profiles.value.find(x => x.id === active.value)?.name || '')

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

// ---- 列表项元信息：流量/到期/更新时间/尺寸/UA 任一存在则显示元信息区 ----
function hasMeta(p) {
  return !!(trafficOf(p) || p.updated_at || p.size || p.ua)
}
function usageOf(p) {
  const t = trafficOf(p)
  if (!t) return ''
  return `${fmtBytes(t.used)} / ${fmtBytes(t.total)}（${Math.round(t.percent)}%）`
}

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
      <div>
        <h2 class="page-title">订阅</h2>
        <p class="page-sub">
          {{ profiles.length }} 个订阅<template v-if="activeName"> · 当前使用：{{ activeName }}</template>
        </p>
      </div>
      <n-button type="primary" @click="openAdd"><template #icon><AppIcon name="plus" :size="14" /></template>添加</n-button>
    </div>

    <n-card v-if="profiles.length === 0" class="pad">
      <n-empty description="还没有订阅，点击右上角「添加」粘贴机场订阅链接" />
    </n-card>

    <n-card v-else>
      <!-- 筛选/操作条：关键字过滤 + 命中计数 + 定时/全部更新（日志页同款布局） -->
      <div class="filter-bar">
        <n-input v-model:value="keyword" size="small" placeholder="过滤订阅名称、地址或 UA…" clearable class="f-kw">
          <template #prefix>
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" />
            </svg>
          </template>
        </n-input>
        <span v-if="keyword" class="hit-count mono">命中 {{ filtered.length }}/{{ profiles.length }}</span>
        <div class="bar-btns">
          <n-button size="small" :title="`定时更新：${schedSummary}，点击配置`" @click="openSchedule">
            <template #icon><AppIcon name="clock" :size="13" /></template>
            定时 · {{ schedSummary }}
          </n-button>
          <n-button size="small" type="primary" ghost :loading="updatingAll" @click="updateAll">
            <template #icon><AppIcon name="refresh" :size="13" /></template>更新
          </n-button>
        </div>
      </div>

      <!-- 订阅列表：不用表格（窄屏横向滚动挤压难看），一个 item 三段式——
           名称行（+已启用标识）、地址行、元信息区（流量/到期/更新时间，有则显示）、操作按钮行居中 -->
      <n-list v-if="filtered.length" hoverable class="sub-list">
        <n-list-item v-for="p in filtered" :key="p.id" :class="{ 'item-on': p.id === active }">
          <div class="sub-item">
            <div class="li-head">
              <span class="li-name" :title="p.name">{{ p.name }}</span>
              <n-tag v-if="p.id === active" size="small" round :bordered="false" type="success">已启用</n-tag>
            </div>
            <div
              class="li-url mono"
              :title="p.source === 'nodes' ? `手动节点 · 模板 ${p.template || '白名单'}` : p.url"
            >
              {{ p.source === 'nodes' ? `手动节点 · 模板 ${p.template || '白名单'}` : p.url }}
            </div>
            <div v-if="hasMeta(p)" class="li-meta">
              <n-progress
                v-if="trafficOf(p)"
                type="line" :percentage="trafficOf(p).percent"
                :show-indicator="false" :height="5" border-radius="3px" class="li-bar"
              />
              <div class="li-meta-row">
                <span v-if="trafficOf(p)" class="mono dim t2">
                  {{ usageOf(p) }}<template v-if="p.expire"> · 到期 {{ fmtExpire(p.expire) }}</template>
                </span>
                <n-tag v-if="p.updated_at" size="small" :bordered="false" type="info" class="upd-tag">
                  {{ fmtTime(p.updated_at) }}
                </n-tag>
                <n-tag v-if="p.size" size="small" :bordered="false" class="upd-tag">{{ fmtBytes(p.size) }}</n-tag>
                <span v-if="p.ua" class="dim t2 li-ua" :title="p.ua">{{ p.ua }}</span>
              </div>
            </div>
            <div class="li-ops">
              <n-button size="small" title="更新" :loading="busyId === p.id" :disabled="updatingAll" @click="update(p)">
                <template #icon><AppIcon name="refresh" :size="13" /></template>更新
              </n-button>
              <n-button size="small" type="error" ghost title="删除" :disabled="busyId === p.id || updatingAll" @click="remove(p)">
                <template #icon><AppIcon name="trash" :size="13" /></template>删除
              </n-button>
              <n-button size="small" title="编辑" :disabled="busyId === p.id || updatingAll" @click="openEdit(p)">
                <template #icon><AppIcon name="edit" :size="13" /></template>编辑
              </n-button>
              <n-button
                v-if="p.id !== active" type="primary" size="small" title="启用"
                :disabled="busyId === p.id || updatingAll" @click="activate(p)"
              >
                <template #icon><AppIcon name="upload" :size="13" /></template>启用
              </n-button>
            </div>
          </div>
        </n-list-item>
      </n-list>
      <n-empty v-else description="没有匹配的订阅" style="padding: 30px 0" />
    </n-card>

    <!-- 添加/编辑订阅弹窗（共用组件，editing 决定模式） -->
    <SubFormModal :open="showModal" :edit="editing" @close="showModal = false" @added="load" @saved="load" />

    <!-- 定时更新配置弹窗 -->
    <n-modal
      preset="card"
      title="定时更新订阅"
      :show="showSchedule"
      :style="{ width: '560px', maxWidth: '94vw' }"
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
.pad :deep(.n-empty) { padding: 34px 0; }

/* 筛选/操作条（日志页同款）：关键字过滤 + 命中计数 + 定时/全部更新 */
.filter-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; flex-wrap: wrap; }
.filter-bar .f-kw { flex: 1; max-width: 380px; min-width: 170px; }
.hit-count { flex: none; color: var(--text-dim); font-size: 11px; }
.bar-btns { display: flex; gap: 8px; margin-left: auto; flex-wrap: wrap; }

/* 订阅列表（n-list）：item 内三段式——名称/地址/元信息/操作按钮行 */
.sub-list { background: transparent; }
.sub-list :deep(.n-list-item) { padding: 13px 16px; }
/* 当前使用中的订阅：accent 淡底标识（与表格时代的 row-active 同语义） */
.sub-list :deep(.n-list-item.item-on) { background: var(--accent-soft); }
.sub-item { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.li-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.li-name { font-weight: 600; font-size: 14px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.li-url { color: var(--text-dim); font-size: 12px; word-break: break-all; }
.li-meta { display: flex; flex-direction: column; gap: 5px; }
.li-bar { width: 100%; }
.li-meta-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }
.t2 { font-size: 11.5px; }
.upd-tag { font-family: var(--mono, ui-monospace, monospace); font-size: 11px; }
.li-ua { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* 操作按钮行：整行居中（用户要求的视觉重心），宽度不够时自动换行仍保持居中 */
.li-ops { display: flex; gap: 8px; justify-content: center; flex-wrap: wrap; margin-top: 3px; }

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

/* ---- 手机/平板：筛选条换行、搜索独占一行；操作按钮等分铺满 item 宽度 ---- */
@media (max-width: 760px) {
  .filter-bar .f-kw { flex: 1 1 100%; max-width: 100%; }
  .bar-btns { margin-left: 0; }
  /* 订阅 item 操作按钮：压小高度与字号，等分铺满一行（≤4 个按钮不换行），内容居中 */
  .sub-list :deep(.n-list-item) { padding: 12px; }
  .li-ops { flex-wrap: nowrap; gap: 6px; }
  .li-ops :deep(.n-button) {
    flex: 1 1 0; min-width: 0;
    height: 26px; font-size: 12px;
    padding-left: 4px; padding-right: 4px;
  }
}
</style>
