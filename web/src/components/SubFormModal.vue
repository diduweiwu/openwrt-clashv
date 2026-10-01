<script setup>
// 添加/编辑订阅弹窗（订阅页与首页共用）：由 open prop 控制显隐
// 添加模式链接输入为 textarea：一行一条，支持粘贴多个批量添加；
// 编辑模式（传入 edit prop）单条回填，保存走 PUT /api/profiles/{id}
import { computed, ref, watch } from 'vue'
import { NButton, NInput, NModal, NSelect } from 'naive-ui'
import { api } from '../api.js'
import AppIcon from './AppIcon.vue'
import { toast } from '../store.js'

const props = defineProps({
  open: Boolean,
  edit: { type: Object, default: null }, // 非 null 时为编辑模式
})
const emit = defineEmits(['close', 'added', 'saved'])

const isEdit = computed(() => !!props.edit)

const name = ref('')
const url = ref('')
const adding = ref(false)
const addProgress = ref('')

// 内置 clash 相关 UA；很多机场按 UA 返回对应格式的配置
const CUSTOM_UA = '__custom__'
const UA_OPTIONS = [
  { value: '', label: '默认（clash-verge/clashv）' },
  { value: 'clash.meta', label: 'clash.meta（mihomo）' },
  { value: 'ClashforWindows/0.20.39', label: 'Clash for Windows' },
  { value: 'ClashMetaForAndroid/2.11.5', label: 'ClashMeta for Android' },
  { value: 'Stash/2.7.3', label: 'Stash' },
  { value: CUSTOM_UA, label: '自定义…' },
]
const uaPick = ref('')
const customUa = ref('')

// 把 UA 值映射到下拉选项：内置项直接选中，其他进「自定义」
function pickUA(ua) {
  if (ua && !UA_OPTIONS.some(o => o.value === ua)) {
    uaPick.value = CUSTOM_UA
    customUa.value = ua
  } else {
    uaPick.value = ua || ''
    customUa.value = ''
  }
}

watch(() => props.open, (v) => {
  if (!v) return
  adding.value = false
  addProgress.value = ''
  if (props.edit) {
    name.value = props.edit.name
    url.value = props.edit.url
    pickUA(props.edit.ua || '')
    return
  }
  name.value = ''
  url.value = ''
  // 上次用的 UA 不是内置项 → 自动选中"自定义"并预填
  api.get('/api/settings').then(s => pickUA(s.custom_ua || '')).catch(() => { /* 拿不到就保持默认 */ })
})

const modalTitle = computed(() => (isEdit.value ? '编辑订阅' : '添加订阅'))
const urlPlaceholder = computed(() =>
  isEdit.value
    ? '订阅链接'
    : '订阅链接，每行一条，可粘贴多行批量添加\nhttps://example.com/subscription')

// 多行且没填备注名时，用链接域名当名字，避免一排「订阅」分不清
function hostOf(line) {
  try { return new URL(line).hostname } catch { return '' }
}
function shortUrl(line) {
  const s = line.replace(/^https?:\/\//, '')
  return s.length > 46 ? s.slice(0, 46) + '…' : s
}

// 添加模式：多行批量；多行且未填备注名时用链接域名当名字
async function add() {
  const lines = url.value.split('\n').map(s => s.trim()).filter(Boolean)
  if (!lines.length) { toast('请输入订阅链接', 'info'); return }
  const ua = uaPick.value === CUSTOM_UA ? customUa.value.trim() : uaPick.value
  if (uaPick.value === CUSTOM_UA && !ua) { toast('请输入自定义 User-Agent', 'info'); return }

  adding.value = true
  const multi = lines.length > 1
  const added = []
  const failed = []
  try {
    for (let i = 0; i < lines.length; i++) {
      if (multi) addProgress.value = `${i + 1}/${lines.length}`
      const n = name.value.trim()
      const useName = multi ? (n ? `${n}-${i + 1}` : hostOf(lines[i])) : n
      try {
        const p = await api.post('/api/profiles', { name: useName, url: lines[i], ua })
        added.push(p)
      } catch (e) {
        failed.push(`${shortUrl(lines[i])}：${e.message}`)
      }
    }
    if (added.length && !failed.length) {
      toast(multi ? `已添加 ${added.length} 个订阅` : '订阅已添加', 'success')
    } else if (added.length) {
      toast(`已添加 ${added.length} 个，${failed.length} 个失败：${failed[0]}`, 'error', 6000)
    } else {
      toast(failed[0] || '添加失败', 'error', 6000)
    }
    if (added.length) {
      emit('added', added[added.length - 1])
      emit('close')
    }
  } finally {
    adding.value = false
    addProgress.value = ''
  }
}

// 编辑模式：单条保存；名称留空沿用原名。地址或 UA 变化时后端自动重新下载
async function saveEdit() {
  adding.value = true
  try {
    const r = await api.put(`/api/profiles/${props.edit.id}`, {
      name: name.value.trim(),
      url: url.value.trim(),
      ua: uaPick.value === CUSTOM_UA ? customUa.value.trim() : uaPick.value,
    })
    toast('订阅已保存' + (r.redownloaded ? '，已按新配置重新下载' + (r.restarted ? '并重载内核' : '') : ''), 'success')
    emit('saved', r.profile)
    emit('close')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    adding.value = false
  }
}

function submit() {
  if (isEdit.value) saveEdit()
  else add()
}
</script>

<template>
  <n-modal
    preset="card"
    :title="modalTitle"
    :show="open"
    :style="{ width: '460px', maxWidth: '94vw' }"
    @update:show="emit('close')"
  >
    <div class="form">
      <n-input v-model:value="name" :placeholder="isEdit ? '备注名（留空保持原名）' : '备注名（可选）'" @keyup.enter="submit" />
      <n-input
        v-model:value="url"
        type="textarea"
        :rows="isEdit ? 2 : 3"
        :placeholder="urlPlaceholder"
        @keyup.enter="submit"
      />
      <n-select v-model:value="uaPick" :options="UA_OPTIONS" />
      <n-input
        v-if="uaPick === CUSTOM_UA"
        v-model:value="customUa"
        placeholder="自定义 User-Agent，如 clash.meta/1.19.31"
        @keyup.enter="submit"
      />
      <p class="page-sub">部分机场按 UA 返回不同格式的配置，更新订阅时沿用添加时的 UA</p>
      <div class="actions">
        <n-button quaternary @click="emit('close')"><template #icon><AppIcon name="close" :size="13" /></template>取消</n-button>
        <n-button v-if="isEdit" type="primary" :loading="adding" @click="submit">
          <template #icon><AppIcon name="check" :size="14" /></template>保存
        </n-button>
        <n-button v-else type="primary" :loading="adding" @click="submit">
          <template #icon><AppIcon name="plus" :size="14" /></template>
          {{ adding && addProgress ? `添加中 ${addProgress}…` : '添加' }}
        </n-button>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.form { display: flex; flex-direction: column; gap: 12px; }
.actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
