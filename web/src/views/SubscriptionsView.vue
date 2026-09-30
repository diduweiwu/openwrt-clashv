<script setup>
// 订阅页：添加（弹窗）/ 更新 / 激活 / 删除
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api.js'
import { store, toast, fmtBytes, fmtTime } from '../store.js'

const route = useRoute()
const router = useRouter()

const profiles = ref([])
const active = ref('')
const showModal = ref(false)
const name = ref('')
const url = ref('')
const adding = ref(false)
const busyId = ref('')

// 内置 clash 相关 UA；很多机场按 UA 返回对应格式的配置
const CUSTOM_UA = '__custom__'
const UA_OPTIONS = [
  { v: '', label: '默认（clash-verge/clashv）' },
  { v: 'clash.meta', label: 'clash.meta（mihomo）' },
  { v: 'ClashforWindows/0.20.39', label: 'Clash for Windows' },
  { v: 'ClashMetaForAndroid/2.11.5', label: 'ClashMeta for Android' },
  { v: 'Stash/2.7.3', label: 'Stash' },
  { v: CUSTOM_UA, label: '自定义…' },
]
const uaPick = ref('')
const customUa = ref('')

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

async function openAdd() {
  name.value = ''
  url.value = ''
  uaPick.value = ''
  customUa.value = ''
  // 上次用的 UA 不是内置项 → 自动选中"自定义"并预填
  try {
    const s = await api.get('/api/settings')
    const last = s.custom_ua || ''
    if (last && !UA_OPTIONS.some(o => o.v === last)) {
      uaPick.value = CUSTOM_UA
      customUa.value = last
    } else if (last) {
      uaPick.value = last
    }
  } catch { /* 拿不到就保持默认 */ }
  showModal.value = true
}

async function add() {
  if (!url.value.trim()) { toast('请输入订阅链接', 'info'); return }
  const ua = uaPick.value === CUSTOM_UA ? customUa.value.trim() : uaPick.value
  if (uaPick.value === CUSTOM_UA && !ua) { toast('请输入自定义 User-Agent', 'info'); return }
  adding.value = true
  try {
    await api.post('/api/profiles', { name: name.value.trim(), url: url.value.trim(), ua })
    toast('订阅已添加', 'success')
    showModal.value = false
    name.value = ''
    url.value = ''
    await load()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    adding.value = false
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
  if (!confirm(`确定删除订阅「${p.name}」？`)) return
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
  if (route.query.add) openAdd()
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
      <button class="primary" @click="openAdd">＋ 添加订阅</button>
    </div>

    <div v-if="profiles.length === 0" class="card empty-hint">
      还没有订阅，点击右上角「添加订阅」粘贴机场订阅链接
    </div>

    <div v-for="p in profiles" :key="p.id" class="card profile-card" :class="{ active: p.id === active }">
      <div class="p-main">
        <div class="p-title">
          <h3>{{ p.name }}</h3>
          <span v-if="p.id === active" class="badge">✓ 使用中</span>
        </div>
        <p class="p-url">{{ p.url }}</p>
        <p class="page-sub">更新于 {{ fmtTime(p.updated_at) }} · {{ fmtBytes(p.size) }}<template v-if="p.ua"> · UA <span class="mono">{{ p.ua }}</span></template></p>
        <div v-if="trafficOf(p)" class="p-traffic">
          <div class="bar"><div class="bar-fill" :style="{ width: trafficOf(p).percent + '%' }"></div></div>
          <span class="mono p-traffic-text">
            {{ fmtBytes(trafficOf(p).used) }} / {{ fmtBytes(trafficOf(p).total) }}（{{ Math.round(trafficOf(p).percent) }}%）
          </span>
        </div>
        <p v-if="p.expire" class="page-sub">到期时间 {{ fmtExpire(p.expire) }}</p>
      </div>
      <div class="p-actions">
        <button class="ghost sm" :disabled="busyId === p.id" @click="update(p)">
          {{ busyId === p.id ? '处理中…' : '更新' }}
        </button>
        <button
          v-if="p.id !== active"
          class="primary sm"
          :disabled="busyId === p.id"
          @click="activate(p)"
        >启用</button>
        <button class="danger sm" :disabled="busyId === p.id" @click="remove(p)">删除</button>
      </div>
    </div>

    <!-- 添加订阅弹窗 -->
    <div v-if="showModal" class="overlay" @click.self="showModal = false">
      <div class="modal">
        <h3>添加订阅</h3>
        <input v-model="name" placeholder="备注名（可选）" @keyup.enter="add">
        <input v-model="url" placeholder="https://example.com/subscription" class="url-input" @keyup.enter="add">
        <div class="ua-row">
          <select v-model="uaPick" class="ua-select">
            <option v-for="o in UA_OPTIONS" :key="o.v" :value="o.v">{{ o.label }}</option>
          </select>
        </div>
        <input
          v-if="uaPick === CUSTOM_UA"
          v-model="customUa"
          placeholder="自定义 User-Agent，如 clash.meta/1.19.31"
          class="url-input"
          @keyup.enter="add"
        >
        <p class="page-sub">部分机场按 UA 返回不同格式的配置，更新订阅时沿用添加时的 UA</p>
        <div class="modal-actions">
          <button class="ghost" @click="showModal = false">取消</button>
          <button class="primary" :disabled="adding" @click="add">
            {{ adding ? '下载中…' : '添加' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.head-row { display: flex; align-items: flex-start; justify-content: space-between; }
.profile-card {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  flex-wrap: wrap;
}
.profile-card.active { border-color: var(--accent); }
.p-main { display: flex; flex-direction: column; gap: 5px; min-width: 0; flex: 1; }
.p-main > * { margin: 0; }
.p-title { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
.p-title h3 { font-size: 15px; }
.p-url { color: var(--text-dim); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 520px; }
.p-traffic { display: flex; align-items: center; gap: 10px; margin-top: 2px; }
.bar { width: 220px; max-width: 45%; height: 5px; border-radius: 3px; background: var(--border); overflow: hidden; }
.bar-fill { height: 100%; border-radius: 3px; background: var(--accent); }
.p-traffic-text { color: var(--text-dim); font-size: 12px; }
.p-actions { display: flex; gap: 8px; flex: none; }
.empty-hint { color: var(--text-dim); text-align: center; padding: 34px 0; }

.overlay {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0, 0, 0, 0.55);
  display: flex; align-items: center; justify-content: center;
  padding: 20px;
}
.modal {
  width: 460px; max-width: 100%;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: var(--shadow);
  padding: 20px;
  display: flex; flex-direction: column; gap: 12px;
}
.modal h3 { font-size: 15px; }
.modal input { width: 100%; box-sizing: border-box; }
.ua-row { display: flex; }
.ua-select { width: 100%; box-sizing: border-box; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
