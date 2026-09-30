<script setup>
import { onMounted, onUnmounted } from 'vue'
import { NConfigProvider } from 'naive-ui'
import Sidebar from './components/Sidebar.vue'
import { store, naiveTheme, naiveOverrides } from './store.js'
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

onMounted(() => {
  refreshStatus()
  armPolling()
})
onUnmounted(() => {
  clearInterval(statusTimer)
})
</script>

<template>
  <n-config-provider class="provider" :theme="naiveTheme" :theme-overrides="naiveOverrides">
    <div class="layout">
      <Sidebar />
      <main class="content">
        <router-view v-slot="{ Component }">
          <transition name="page" mode="out-in">
            <component :is="Component" :key="$route.fullPath" />
          </transition>
        </router-view>
      </main>
    </div>
  </n-config-provider>
</template>

<style scoped>
/* NConfigProvider 自带一层 div，需接上 #app 的 height:100% 链，侧栏才能占满全高 */
.provider { height: 100%; }
.layout { display: flex; height: 100%; }
.content {
  flex: 1;
  overflow-y: auto;
  padding: 26px 30px 40px;
}
@media (max-width: 760px) {
  .layout { flex-direction: column; }
  .content { padding: 18px 14px 30px; }
}
</style>
