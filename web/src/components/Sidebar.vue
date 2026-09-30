<script setup>
import { computed } from 'vue'
import { NButton, NDropdown } from 'naive-ui'
import { themeMode, resolvedTheme, applyTheme } from '../theme.js'
import { store } from '../store.js'

const navs = [
  { to: '/', label: '首页', icon: 'home' },
  { to: '/proxies', label: '代理', icon: 'proxy' },
  { to: '/rules', label: '规则', icon: 'rules' },
  { to: '/connections', label: '连接', icon: 'conns' },
  { to: '/profiles', label: '订阅', icon: 'profiles' },
  { to: '/logs', label: '日志', icon: 'logs' },
  { to: '/settings', label: '设置', icon: 'settings' },
]

// ---- 主题下拉 ----
const themeOptions = [
  { label: '☾ 深色', key: 'dark' },
  { label: '☀ 浅色', key: 'light' },
  { label: '◐ 跟随系统', key: 'system' },
]

// 触发按钮上显示当前实际配色：跟随系统时展示解析结果
const themeLabel = computed(() => {
  if (themeMode.value === 'system') {
    return `◐ 跟随系统 · ${resolvedTheme.value === 'dark' ? '深色' : '浅色'}`
  }
  return themeOptions.find(o => o.key === themeMode.value).label
})

function pickTheme(v) {
  themeMode.value = v
  applyTheme()
}
</script>

<template>
  <aside class="sidebar">
    <div class="logo">
      <!-- ClashV logo：Clash 猫（线条用 currentColor 继承侧栏白色，粗细按小尺寸展示微调） -->
      <svg viewBox="0 0 48 48" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
        <path stroke-linejoin="round" d="M27.19,42.5a89.0444,89.0444,0,0,1-14.6813-1.5725S13.94,12.3721,17.9209,5.5357c-.13-.297,2.9919,1.2125,4.4218,6.2665a25.5569,25.5569,0,0,1,4.8471-.47"/>
        <ellipse cx="21.2404" cy="20.3089" rx="1.6708" ry="2.1301"/>
        <path stroke-linejoin="round" d="M27.19,42.5a89.0444,89.0444,0,0,0,14.6813-1.5725S40.44,12.3721,36.4583,5.5357c.03-.2006-3.59,1.7549-4.4218,6.2665a25.5582,25.5582,0,0,0-4.8471-.47"/>
        <ellipse cx="33.1398" cy="20.3089" rx="1.6708" ry="2.1301"/>
        <path stroke-miterlimit="5.7143" d="M12.5083,40.927C10.5777,40.6,7.56,40.6178,6.4685,37.44c-1.0674-3.107.4377-6.6708,3.7411-7.0453"/>
        <path stroke-miterlimit="5.7143" d="M25.4634,26.3872a1.4666,1.4666,0,0,0,1.4726-1.4725"/>
        <path stroke-miterlimit="5.7143" d="M28.4091,26.3872a1.4666,1.4666,0,0,1-1.4726-1.4725"/>
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
          <template v-else-if="n.icon === 'rules'">
            <path d="M12 3.5 18.5 6v5.2c0 4.3-2.8 7.2-6.5 8.8-3.7-1.6-6.5-4.5-6.5-8.8V6L12 3.5Z" />
            <path d="m9.2 11.8 2 2 3.6-4" />
          </template>
          <template v-else-if="n.icon === 'conns'">
            <path d="M3 12h4l2.5-6.5 4.5 13L16.5 12H21" />
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
        <span class="dot" :class="{ on: store.status?.running, wait: !store.status?.running && store.status?.starting }"></span>
        <span class="status-text">{{ store.status?.running ? '运行中' : store.status?.starting ? '启动中…' : '已停止' }}</span>
      </div>
      <n-dropdown trigger="click" placement="top-start" :options="themeOptions" @select="pickTheme">
        <n-button quaternary size="small" class="theme-btn">{{ themeLabel }}</n-button>
      </n-dropdown>
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
  display: flex; align-items: center; gap: 26px;
  padding: 10px 12px;
  border-radius: 10px;
  color: rgba(255, 255, 255, 0.62);
  text-decoration: none;
  font-size: 13.5px;
  transition: background 0.15s, color 0.15s;
}
/* 标题字符间隔开（HTML 连续空格会塌缩，用 letter-spacing 做出两格空格观感） */
.nav-item span { letter-spacing: 0.5em; }
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
.dot.wait { background: var(--orange); box-shadow: 0 0 6px var(--orange); animation: blink 1s ease-in-out infinite; }
@keyframes blink { 50% { opacity: 0.35; } }
.theme-btn {
  /* 与上方 .status-line 的 0 8px 内边距对齐：图标落在状态点同一垂直线上 */
  width: 100%;
  justify-content: flex-start;
  padding-left: 8px;
  color: rgba(255, 255, 255, 0.55);
  font-size: 12px;
  white-space: nowrap;
}
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
  .theme-btn { width: auto; justify-content: center; padding-left: 0; }
}
</style>
