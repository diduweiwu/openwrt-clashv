<script setup>
// 节点选择弹窗：点击组后弹出，选中即切换
import { onMounted, ref } from 'vue'
import { NModal } from 'naive-ui'
import { api } from '../api.js'
import { toast, delayColor } from '../store.js'

const props = defineProps({
  group: { type: Object, required: true }, // {name, now, all, type}
  proxies: { type: Object, default: () => ({}) }, // 全量节点表，取延迟用
})
const emit = defineEmits(['close', 'selected'])

// 组件由父级 v-if 挂载；显隐在内部管理，退场动画播完（after-leave）再通知父级卸载
const visible = ref(false)
onMounted(() => { visible.value = true })

function close() {
  visible.value = false
}

function delayOf(nodeName) {
  const p = props.proxies[nodeName]
  const h = p?.history
  if (Array.isArray(h) && h.length) return h[h.length - 1].delay
  return 0
}

async function pick(nodeName) {
  try {
    await api.put('/api/proxies/' + encodeURIComponent(props.group.name), { name: nodeName })
    toast(`「${props.group.name}」已切换到 ${nodeName}`, 'success')
    emit('selected', nodeName)
    close()
  } catch (e) {
    toast(e.message, 'error')
  }
}
</script>

<template>
  <n-modal
    preset="card"
    :show="visible"
    :style="{ width: '480px', maxWidth: '94vw' }"
    @update:show="close"
    @after-leave="emit('close')"
  >
    <template #header>
      <span class="title">{{ group.name }}</span>
      <span class="sub">{{ group.type }} · {{ group.all.length }} 个节点</span>
    </template>
    <div class="list">
      <button
        v-for="node in group.all"
        :key="node"
        class="node"
        :class="{ current: node === group.now }"
        @click="pick(node)"
      >
        <span class="name">{{ node }}</span>
        <span class="delay mono" :style="{ color: delayColor(delayOf(node)) }">
          {{ delayOf(node) > 0 ? delayOf(node) + ' ms' : node === group.now ? '当前' : '' }}
        </span>
      </button>
    </div>
  </n-modal>
</template>

<style scoped>
.title { font-size: 15px; }
.sub { color: var(--text-dim); font-size: 12px; font-weight: 400; margin-left: 8px; }
.list { display: flex; flex-direction: column; gap: 4px; max-height: 60vh; overflow-y: auto; }
.node {
  display: flex; align-items: center; justify-content: space-between;
  width: 100%; text-align: left;
  padding: 10px 13px; border-radius: 10px;
  background: transparent; border: none; cursor: pointer;
  font: inherit; color: inherit;
}
.node:hover { background: var(--hover); }
.node.current { background: var(--accent-soft); }
.name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-right: 12px; }
.delay { font-size: 12px; flex: none; }
</style>
