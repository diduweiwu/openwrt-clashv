<script setup>
import { onMounted, onUnmounted } from 'vue'
import Sidebar from './components/Sidebar.vue'
import { store } from './store.js'
import { api } from './api.js'

let statusTimer = null
let lastStarting = null

async function refreshStatus() {
  try {
    store.status = await api.get('/api/status')
    // 启动窗口期把轮询加密到 1s，让「启动中…→运行中」的切换即时可见
    const starting = !!store.status?.starting
    if (starting !== lastStarting) {
      lastStarting = starting
      armPolling()
    }
  } catch (e) {
    // 静默，避免后端未启动时刷屏
  }
}

function armPolling() {
  if (statusTimer) clearInterval(statusTimer)
  statusTimer = setInterval(refreshStatus, store.status?.starting ? 1000 : 5000)
}

// 各类型 toast 的图标（描边风格，与侧边栏图标一致）
const ICON_PATHS = {
  success: 'M12 3.2a8.8 8.8 0 1 0 0 17.6 8.8 8.8 0 0 0 0-17.6Zm4.1 6.2-5.3 5.9-2.9-3',
  error: 'M12 3.2a8.8 8.8 0 1 0 0 17.6 8.8 8.8 0 0 0 0-17.6ZM8.8 8.8l6.4 6.4M15.2 8.8l-6.4 6.4',
  warning: 'M12 3.5 21.3 19.7H2.7L12 3.5ZM12 9.8v4M12 16.4v.7',
  info: 'M12 3.2a8.8 8.8 0 1 0 0 17.6 8.8 8.8 0 0 0 0-17.6ZM12 11v5M12 7.5v.7',
}

function iconFor(type) {
  const d = ICON_PATHS[type] || ICON_PATHS.info
  return `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="${d}"/></svg>`
}

onMounted(() => {
  refreshStatus()
  armPolling()
})
onUnmounted(() => {
  clearInterval(statusTimer)
})
</script>

<template>
  <div class="layout">
    <Sidebar />
    <main class="content">
      <router-view v-slot="{ Component }">
        <transition name="page" mode="out-in">
          <component :is="Component" :key="$route.fullPath" />
        </transition>
      </router-view>
    </main>
    <div class="toasts">
      <transition-group name="toast">
        <div v-for="t in store.toasts" :key="t.id" class="toast" :class="t.type">
          <span class="t-icon" v-html="iconFor(t.type)"></span>
          <span class="t-msg">{{ t.message }}</span>
        </div>
      </transition-group>
    </div>
  </div>
</template>

<style scoped>
.layout { display: flex; height: 100%; }
.content {
  flex: 1;
  overflow-y: auto;
  padding: 26px 30px 40px;
}
.toasts {
  position: fixed;
  top: 18px;
  right: 18px;
  z-index: 999;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.toast {
  display: flex;
  align-items: center;
  gap: 11px;
  background: var(--bg-card);
  color: var(--text);
  border: 1px solid var(--border);
  border-left: 3px solid var(--accent);
  border-radius: 12px;
  padding: 13px 18px 13px 14px;
  box-shadow: var(--shadow);
  max-width: 420px;
  min-width: 240px;
  font-size: 14px;
  line-height: 1.5;
}
.t-icon {
  flex: none;
  width: 30px; height: 30px;
  border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--accent-soft);
  color: var(--accent);
}
.t-msg { min-width: 0; word-break: break-word; }
.toast.success { border-left-color: var(--green); }
.toast.success .t-icon { background: rgba(63, 185, 111, 0.15); color: var(--green); }
.toast.error { border-left-color: var(--red); }
.toast.error .t-icon { background: rgba(224, 85, 85, 0.15); color: var(--red); }
.toast.warning { border-left-color: var(--orange); }
.toast.warning .t-icon { background: rgba(232, 161, 60, 0.15); color: var(--orange); }

.toast-enter-active, .toast-leave-active { transition: opacity 0.22s, transform 0.22s; }
.toast-enter-from { opacity: 0; transform: translateX(28px); }
.toast-leave-to { opacity: 0; transform: translateX(16px); }
.toast-move { transition: transform 0.22s; }

@media (max-width: 760px) {
  .layout { flex-direction: column; }
  .content { padding: 18px 14px 30px; }
}
</style>
