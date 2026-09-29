<script setup>
// 订阅页：添加 / 更新 / 激活 / 删除
import { onMounted, ref } from 'vue'
import { api } from '../api.js'
import { store, toast, fmtBytes, fmtTime } from '../store.js'

const profiles = ref([])
const active = ref('')
const name = ref('')
const url = ref('')
const adding = ref(false)
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

async function add() {
  if (!url.value.trim()) { toast('请输入订阅链接', 'info'); return }
  adding.value = true
  try {
    await api.post('/api/profiles', { name: name.value.trim(), url: url.value.trim() })
    toast('订阅已添加', 'success')
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

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="card add-card">
      <h3>添加订阅</h3>
      <div class="add-row">
        <input v-model="name" placeholder="备注名（可选）" style="width: 180px">
        <input v-model="url" placeholder="https://example.com/subscription" class="url-input" @keyup.enter="add">
        <button class="primary" :disabled="adding" @click="add">
          {{ adding ? '下载中…' : '添加' }}
        </button>
      </div>
    </div>

    <div v-if="profiles.length === 0" class="card empty-hint">
      还没有订阅，先在上方粘贴订阅链接添加
    </div>

    <div v-for="p in profiles" :key="p.id" class="card profile-card" :class="{ active: p.id === active }">
      <div class="p-main">
        <div class="p-title">
          <h3>{{ p.name }}</h3>
          <span v-if="p.id === active" class="badge">✓ 使用中</span>
        </div>
        <p class="p-url">{{ p.url }}</p>
        <p class="page-sub">更新于 {{ fmtTime(p.updated_at) }} · {{ fmtBytes(p.size) }}</p>
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
  </div>
</template>

<style scoped>
.add-card { display: flex; flex-direction: column; gap: 12px; }
.add-row { display: flex; gap: 10px; flex-wrap: wrap; }
.url-input { flex: 1; min-width: 220px; }
.profile-card {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  flex-wrap: wrap;
}
.profile-card.active { border-color: var(--accent); }
.p-main { display: flex; flex-direction: column; gap: 5px; min-width: 0; flex: 1; }
.p-title { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
.p-title h3 { font-size: 15px; }
.p-url { color: var(--text-dim); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 520px; }
.p-actions { display: flex; gap: 8px; flex: none; }
.empty-hint { color: var(--text-dim); text-align: center; padding: 34px 0; }
</style>
