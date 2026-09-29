<script setup>
import { onMounted, onUnmounted } from 'vue'
import Sidebar from './components/Sidebar.vue'
import { store } from './store.js'
import { api } from './api.js'

let statusTimer = null

async function refreshStatus() {
  try {
    store.status = await api.get('/api/status')
  } catch (e) {
    // 静默，避免后端未启动时刷屏
  }
}

onMounted(() => {
  refreshStatus()
  statusTimer = setInterval(refreshStatus, 5000)
})
onUnmounted(() => {
  clearInterval(statusTimer)
})
</script>

<template>
  <div class="layout">
    <Sidebar />
    <main class="content">
      <router-view :key="$route.fullPath" />
    </main>
    <div class="toasts">
      <transition-group name="fade">
        <div v-for="t in store.toasts" :key="t.id" class="toast" :class="t.type">
          {{ t.message }}
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
  gap: 8px;
}
.toast {
  background: var(--bg-card);
  color: var(--text);
  border: 1px solid var(--border);
  border-left: 3px solid var(--accent);
  border-radius: 10px;
  padding: 10px 16px;
  box-shadow: var(--shadow);
  max-width: 340px;
  font-size: 13px;
}
.toast.error { border-left-color: var(--red); }
.toast.success { border-left-color: var(--green); }
@media (max-width: 760px) {
  .layout { flex-direction: column; }
  .content { padding: 18px 14px 30px; }
}
</style>
