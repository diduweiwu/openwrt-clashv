<script setup>
// 节点选择弹窗：点击组后弹出，选中即切换
import { onMounted, ref } from 'vue'
import { api } from '../api.js'
import { toast, delayColor } from '../store.js'

const props = defineProps({
  group: { type: Object, required: true }, // {name, now, all, type}
  proxies: { type: Object, default: () => ({}) }, // 全量节点表，取延迟用
})
const emit = defineEmits(['close', 'selected'])

// 组件由父级 v-if 挂载；可见性在内部管理，退场动画播完再通知父级卸载
const visible = ref(false)
onMounted(() => { visible.value = true })

function close() {
  visible.value = false
  setTimeout(() => emit('close'), 170)
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
  <teleport to="body">
    <transition name="modal">
      <div v-if="visible" class="mask" @click.self="close">
        <div class="sheet modal-panel">
        <div class="head">
          <div>
            <h3>{{ group.name }}</h3>
            <span class="sub">{{ group.type }} · {{ group.all.length }} 个节点</span>
          </div>
          <button class="ghost sm" @click="close">✕</button>
        </div>
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
      </div>
    </div>
    </transition>
  </teleport>
</template>

<style scoped>
.mask {
  position: fixed; inset: 0; z-index: 100;
  background: rgba(8, 10, 16, 0.55);
  display: flex; align-items: center; justify-content: center;
  padding: 20px;
}
.sheet {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 16px;
  width: 480px; max-width: 94vw; max-height: 76vh;
  display: flex; flex-direction: column;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.4);
}
.head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 18px 12px;
  border-bottom: 1px solid var(--border);
}
.sub { color: var(--text-dim); font-size: 12px; }
.list { overflow-y: auto; padding: 10px; display: flex; flex-direction: column; gap: 4px; }
.node {
  display: flex; align-items: center; justify-content: space-between;
  width: 100%; text-align: left;
  padding: 10px 13px; border-radius: 10px;
  background: transparent;
}
.node:hover { background: var(--hover); }
.node.current { background: var(--accent-soft); }
.name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-right: 12px; }
.delay { font-size: 12px; flex: none; }
</style>
