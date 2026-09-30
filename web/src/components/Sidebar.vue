<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { themeMode, resolvedTheme, applyTheme } from '../theme.js'
import { store } from '../store.js'

const navs = [
  { to: '/', label: '首页', icon: 'home' },
  { to: '/proxies', label: '代理', icon: 'proxy' },
  { to: '/profiles', label: '订阅', icon: 'profiles' },
  { to: '/logs', label: '日志', icon: 'logs' },
  { to: '/settings', label: '设置', icon: 'settings' },
]

// ---- 主题下拉 ----
const themeOpen = ref(false)
const themePicker = ref(null)

const themeOptions = [
  { value: 'dark', label: '深色', icon: '☾' },
  { value: 'light', label: '浅色', icon: '☀' },
  { value: 'system', label: '跟随系统', icon: '◐' },
]

// 触发按钮上显示当前实际配色：跟随系统时展示解析结果
const themeLabel = computed(() => {
  if (themeMode.value === 'system') {
    return `◐ 跟随系统 · ${resolvedTheme.value === 'dark' ? '深色' : '浅色'}`
  }
  const opt = themeOptions.find(o => o.value === themeMode.value)
  return `${opt.icon} ${opt.label}`
})

function pickTheme(v) {
  themeMode.value = v
  applyTheme()
  themeOpen.value = false
}

function onDocClick(e) {
  if (themePicker.value && !themePicker.value.contains(e.target)) themeOpen.value = false
}
onMounted(() => document.addEventListener('click', onDocClick))
onUnmounted(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <aside class="sidebar">
    <div class="logo">
      <svg viewBox="0 0 24 24" width="26" height="26" fill="none">
        <path d="M13 2 4.5 13.5H11L9.5 22 19 10h-6.5L13 2Z" fill="var(--accent)" stroke="var(--accent)" stroke-width="1.4" stroke-linejoin="round"/>
      </svg>
      <span>ClashV</span>
    </div>

    <nav>
      <router-link v-for="n in navs" :key="n.to" :to="n.to" class="nav-item" active-class="active">
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <template v-if="n.icon === 'home'">
            <path d="M3 10.5 12 3l9 7.5" /><path d="M5 9.5V21h14V9.5" />
          </template>
          <template v-else-if="n.icon === 'proxy'">
            <rect x="3.5" y="3.5" width="7" height="7" rx="1.8" /><rect x="13.5" y="3.5" width="7" height="7" rx="1.8" />
            <rect x="3.5" y="13.5" width="7" height="7" rx="1.8" /><rect x="13.5" y="13.5" width="7" height="7" rx="1.8" />
          </template>
          <template v-else-if="n.icon === 'profiles'">
            <path d="M6 3.5h9l3.5 3.5v13.5H6z" /><path d="M9 12h6M9 16h6M9 8h3" />
          </template>
          <template v-else-if="n.icon === 'logs'">
            <path d="M5 4h14v16H5z" /><path d="M8.5 9h7M8.5 13h7M8.5 17h4" />
          </template>
          <template v-else>
            <circle cx="12" cy="12" r="3.2" />
            <path d="M12 2.8v2.4M12 18.8v2.4M2.8 12h2.4M18.8 12h2.4M5.5 5.5l1.7 1.7M16.8 16.8l1.7 1.7M18.5 5.5l-1.7 1.7M7.2 16.8l-1.7 1.7" />
          </template>
        </svg>
        <span>{{ n.label }}</span>
      </router-link>
    </nav>

    <div class="bottom">
      <div class="status-line">
        <span class="dot" :class="{ on: store.status?.running }"></span>
        <span class="status-text">{{ store.status?.running ? '运行中' : '已停止' }}</span>
      </div>
      <div ref="themePicker" class="theme-picker">
        <button class="ghost sm theme-btn" @click="themeOpen = !themeOpen" @keydown.escape="themeOpen = false">
          {{ themeLabel }}
        </button>
        <transition name="fade">
          <div v-if="themeOpen" class="theme-menu">
            <button
              v-for="o in themeOptions"
              :key="o.value"
              class="theme-opt"
              :class="{ current: o.value === themeMode }"
              @click="pickTheme(o.value)"
            >
              <span class="opt-icon">{{ o.icon }}</span>
              <span>{{ o.label }}</span>
              <span v-if="o.value === themeMode" class="opt-check">✓</span>
            </button>
          </div>
        </transition>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 208px;
  flex: none;
  background: var(--sidebar);
  display: flex;
  flex-direction: column;
  padding: 20px 14px;
  gap: 24px;
}
.logo {
  display: flex; align-items: center; gap: 9px;
  font-size: 17px; font-weight: 700; color: #fff;
  padding: 0 8px;
  letter-spacing: 0.3px;
}
nav { display: flex; flex-direction: column; gap: 4px; flex: 1; }
.nav-item {
  display: flex; align-items: center; gap: 11px;
  padding: 10px 12px;
  border-radius: 10px;
  color: rgba(255, 255, 255, 0.62);
  text-decoration: none;
  font-size: 13.5px;
  transition: background 0.15s, color 0.15s;
}
.nav-item:hover { background: rgba(255, 255, 255, 0.06); color: #fff; }
.nav-item.active { background: var(--accent); color: #fff; }
.bottom { display: flex; flex-direction: column; gap: 10px; }
.status-line {
  display: flex; align-items: center; gap: 8px;
  color: rgba(255, 255, 255, 0.55); font-size: 12.5px; padding: 0 8px;
}
.dot {
  width: 8px; height: 8px; border-radius: 50%;
  background: #5a5f6d;
}
.dot.on { background: var(--green); box-shadow: 0 0 6px var(--green); }
.theme-btn {
  color: rgba(255, 255, 255, 0.55);
  border-color: rgba(255, 255, 255, 0.12);
  font-size: 12px;
  width: 100%;
  white-space: nowrap;
}
.theme-picker { position: relative; }
.theme-menu {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 0; right: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow);
  padding: 4px;
  display: flex; flex-direction: column; gap: 2px;
  z-index: 60;
}
.theme-opt {
  display: flex; align-items: center; gap: 8px;
  background: transparent;
  border: none; border-radius: 7px;
  padding: 7px 10px;
  font-size: 12.5px;
  color: var(--text);
  text-align: left;
}
.theme-opt:hover { background: var(--hover); }
.theme-opt.current { color: var(--accent); background: var(--accent-soft); }
.opt-check { margin-left: auto; font-size: 11px; }
@media (max-width: 760px) {
  .sidebar {
    width: 100%; flex-direction: row; align-items: center;
    padding: 10px 14px; gap: 12px;
  }
  .logo { font-size: 15px; gap: 6px; }
  nav { flex-direction: row; flex: 1; }
  .nav-item { padding: 7px 10px; }
  .nav-item span { display: none; }
  .bottom { flex-direction: row; align-items: center; }
  .status-text { display: none; }
  .theme-btn { width: auto; }
  /* 顶栏场景下拉改为向下展开 */
  .theme-menu { bottom: auto; top: calc(100% + 8px); }
}
</style>
