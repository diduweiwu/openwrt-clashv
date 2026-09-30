<script setup>
// 添加订阅弹窗（订阅页与首页共用）：名称 / 链接 / User-Agent 选择
import { onMounted, ref } from 'vue'
import { api } from '../api.js'
import { toast } from '../store.js'

const emit = defineEmits(['close', 'added'])

const name = ref('')
const url = ref('')
const adding = ref(false)

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

onMounted(async () => {
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
})

async function add() {
  if (!url.value.trim()) { toast('请输入订阅链接', 'info'); return }
  const ua = uaPick.value === CUSTOM_UA ? customUa.value.trim() : uaPick.value
  if (uaPick.value === CUSTOM_UA && !ua) { toast('请输入自定义 User-Agent', 'info'); return }
  adding.value = true
  try {
    const p = await api.post('/api/profiles', { name: name.value.trim(), url: url.value.trim(), ua })
    toast('订阅已添加', 'success')
    emit('added', p)
    emit('close')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    adding.value = false
  }
}
</script>

<template>
  <transition name="modal">
    <div class="overlay" @click.self="emit('close')">
      <div class="modal modal-panel">
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
        <button class="ghost" @click="emit('close')">取消</button>
        <button class="primary" :disabled="adding" @click="add">
          {{ adding ? '下载中…' : '添加' }}
        </button>
      </div>
    </div>
  </div>
  </transition>
</template>

<style scoped>
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
