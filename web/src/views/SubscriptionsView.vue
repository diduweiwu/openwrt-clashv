<script setup>
// 订阅页：添加（弹窗，共用 SubFormModal）/ 更新 / 激活 / 删除
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { NButton, NCard, NEmpty, NProgress, NTag } from 'naive-ui'
import { api } from '../api.js'
import { store, toast, ask, fmtBytes, fmtTime } from '../store.js'
import SubFormModal from '../components/SubFormModal.vue'

const route = useRoute()

const profiles = ref([])
const active = ref('')
const showModal = ref(false)
const busyId = ref('')

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

onMounted(() => {
  if (route.query.add) showModal.value = true
  load()
})
</script>

<template>
  <div class="page">
    <div class="head-row">
      <div>
        <h1 class="page-title">订阅</h1>
        <span class="page-sub">{{ profiles.length ? profiles.length + ' 个订阅' : '管理订阅源' }}</span>
      </div>
      <n-button type="primary" @click="showModal = true">＋ 添加订阅</n-button>
    </div>

    <n-card v-if="profiles.length === 0" class="pad">
      <n-empty description="还没有订阅，点击右上角「添加订阅」粘贴机场订阅链接" />
    </n-card>

    <n-card v-for="p in profiles" :key="p.id" class="profile-card" :class="{ active: p.id === active }">
      <div class="p-main">
        <div class="p-title">
          <h3>{{ p.name }}</h3>
          <n-tag v-if="p.id === active" size="small" round :bordered="false">✓ 使用中</n-tag>
        </div>
        <p class="p-url">{{ p.url }}</p>
        <p class="page-sub">更新于 {{ fmtTime(p.updated_at) }} · {{ fmtBytes(p.size) }}<template v-if="p.ua"> · UA <span class="mono">{{ p.ua }}</span></template></p>
        <div v-if="trafficOf(p)" class="p-traffic">
          <n-progress
            class="bar"
            type="line"
            :percentage="trafficOf(p).percent"
            :show-indicator="false"
            :height="5"
            border-radius="3px"
          />
          <span class="mono page-sub">
            {{ fmtBytes(trafficOf(p).used) }} / {{ fmtBytes(trafficOf(p).total) }}（{{ Math.round(trafficOf(p).percent) }}%）
          </span>
        </div>
        <p v-if="p.expire" class="page-sub">到期时间 {{ fmtExpire(p.expire) }}</p>
      </div>
      <div class="p-actions">
        <n-button size="small" :loading="busyId === p.id" @click="update(p)">更新</n-button>
        <n-button v-if="p.id !== active" type="primary" size="small" :disabled="busyId === p.id" @click="activate(p)">
          启用
        </n-button>
        <n-button size="small" type="error" ghost :disabled="busyId === p.id" @click="remove(p)">删除</n-button>
      </div>
    </n-card>

    <!-- 添加订阅弹窗（共用组件） -->
    <SubFormModal :open="showModal" @close="showModal = false" @added="load" />
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; }
.pad :deep(.n-empty) { padding: 34px 0; }
.profile-card :deep(.n-card__content) { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.profile-card.active { border-color: var(--accent); }
.p-main { display: flex; flex-direction: column; gap: 5px; min-width: 0; flex: 1; }
.p-main > * { margin: 0; }
.p-title { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
.p-title h3 { font-size: 15px; }
.p-url { color: var(--text-dim); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 520px; }
.p-traffic { display: flex; align-items: center; gap: 10px; margin-top: 2px; }
.bar { width: 220px; max-width: 45%; }
.p-actions { display: flex; gap: 8px; flex: none; }
</style>
