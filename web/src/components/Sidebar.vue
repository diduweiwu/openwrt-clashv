<script setup>
import { computed, ref } from 'vue'
import { NButton, NDropdown } from 'naive-ui'
import { themeMode, resolvedTheme, applyTheme } from '../theme.js'
import { store } from '../store.js'
import AppIcon from './AppIcon.vue'

// ---- 侧栏折叠：收起后只留图标导航与 logo 小图，状态记忆在 localStorage ----
const COLLAPSE_KEY = 'clashv_sidebar_collapsed'
const collapsed = ref(localStorage.getItem(COLLAPSE_KEY) === '1')

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  localStorage.setItem(COLLAPSE_KEY, collapsed.value ? '1' : '0')
}

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

// 触发按钮上显示当前实际配色：跟随系统时展示解析结果（窄侧栏用短文案）
const themeLabel = computed(() => {
  if (collapsed.value) return '◐'
  if (themeMode.value === 'system') {
    return `◐ 系统 · ${resolvedTheme.value === 'dark' ? '深色' : '浅色'}`
  }
  return themeOptions.find(o => o.key === themeMode.value).label
})

function pickTheme(v) {
  themeMode.value = v
  applyTheme()
}
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
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
      <router-link v-for="n in navs" :key="n.to" :to="n.to" class="nav-item" active-class="active" :title="collapsed ? n.label : null">
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
            <path d="M14 3.5H7a1 1 0 0 0-1 1v15a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7.5Z" />
            <path d="M14 3.5v4h4" /><path d="M9 12.5h6" /><path d="M9 16h6" />
          </template>
          <template v-else-if="n.icon === 'logs'">
            <path d="M5 4h14v16H5z" /><path d="M8.5 9h7M8.5 13h7M8.5 17h4" />
          </template>
          <template v-else-if="n.icon === 'settings'">
            <!-- 齿轮：lucide settings 外形（齿轮轮廓 + 中孔），避免误认成太阳 -->
            <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
            <circle cx="12" cy="12" r="3" />
          </template>
        </svg>
        <span>{{ n.label }}</span>
      </router-link>
    </nav>

    <div class="bottom">
      <!-- 连接/CPU/内存迷你卡：数据来自全局每秒流量与 5s 状态轮询 -->
      <div class="side-stats">
        <div class="ss">
          <span class="ss-k"><AppIcon name="cpu" :size="11" />CPU</span>
          <span class="ss-v mono">{{ store.status?.running && store.status?.cpu != null ? Math.round(store.status.cpu) + '%' : '—' }}</span>
        </div>
        <div class="ss">
          <span class="ss-k"><AppIcon name="database" :size="11" />内存</span>
          <span class="ss-v mono">{{ store.status?.running && store.traffic.memory_mb ? store.traffic.memory_mb.toFixed(1) + ' MB' : '—' }}</span>
        </div>
        <div class="ss">
          <span class="ss-k"><AppIcon name="link" :size="11" />连接</span>
          <span class="ss-v mono">{{ store.status?.running ? store.traffic.connections : '—' }}</span>
        </div>
      </div>
      <div class="status-line">
        <span class="dot" :class="{ on: store.status?.running, wait: !store.status?.running && store.status?.starting }"></span>
        <span class="status-text">{{ store.status?.running ? '运行中' : store.status?.starting ? '启动中…' : '已停止' }}</span>
      </div>
      <n-dropdown trigger="click" placement="top-start" :options="themeOptions" @select="pickTheme">
        <n-button quaternary size="small" class="theme-btn">{{ themeLabel }}</n-button>
      </n-dropdown>
      <!-- 折叠开关：收起后仅剩箭头图标，logo 区只留小图 -->
      <n-button quaternary size="small" class="collapse-btn" :title="collapsed ? '展开菜单' : '收起菜单'" @click="toggleCollapsed">
        <template #icon>
          <AppIcon :name="collapsed ? 'chevron-right' : 'chevron-left'" :size="14" />
        </template>
        <span class="ct-text">收起菜单</span>
      </n-button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 176px;
  flex: none;
  background: var(--sidebar);
  display: flex;
  flex-direction: column;
  padding: 20px 26px;
  gap: 24px;
  transition: width 0.15s ease;
}
.logo {
  display: flex; align-items: center; gap: 9px;
  font-size: 17px; font-weight: 700; color: #fff;
  padding: 0 8px;
  letter-spacing: 0.3px;
}
nav { display: flex; flex-direction: column; gap: 4px; flex: 1; }
.nav-item {
  display: flex; align-items: center; gap: 15px;
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
/* 连接/CPU/内存迷你卡：细分隔线 + 三行小卡，置于运行状态上方 */
.side-stats {
  display: flex; flex-direction: column; gap: 6px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.09);
}
.ss {
  display: flex; align-items: center; justify-content: space-between;
  background: rgba(255, 255, 255, 0.05);
  border-radius: 8px;
  padding: 6px 9px;
}
.ss-k { display: flex; align-items: center; gap: 5px; color: rgba(255, 255, 255, 0.45); font-size: 11px; }
.ss-v { color: rgba(255, 255, 255, 0.85); font-size: 11.5px; font-weight: 600; }
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
/* 折叠开关：与主题按钮同款左对齐排布 */
.collapse-btn {
  width: 100%;
  justify-content: flex-start;
  padding-left: 8px;
  color: rgba(255, 255, 255, 0.55);
  font-size: 12px;
  white-space: nowrap;
}
.collapse-btn:hover { color: #fff; }

/* ---- 收起态（仅桌面端）：只留图标导航 + logo 小图，底部缩成三枚小圆钮 ---- */
@media (min-width: 761px) {
  .sidebar.collapsed {
    width: 64px;
    padding-left: 12px;
    padding-right: 12px;
  }
  .sidebar.collapsed .logo { justify-content: center; padding: 0; }
  .sidebar.collapsed .logo span { display: none; }
  .sidebar.collapsed .nav-item { justify-content: center; gap: 0; padding: 10px 0; }
  .sidebar.collapsed .nav-item span { display: none; }
  .sidebar.collapsed .side-stats { display: none; }
  .sidebar.collapsed .status-line { justify-content: center; padding: 0; }
  .sidebar.collapsed .status-text { display: none; }
  .sidebar.collapsed .theme-btn,
  .sidebar.collapsed .collapse-btn { justify-content: center; padding-left: 0; }
  .sidebar.collapsed .ct-text { display: none; }
}
@media (max-width: 760px) {
  .sidebar {
    width: 100%; flex-direction: row; align-items: center;
    padding: 10px 14px; gap: 12px;
    flex-wrap: wrap; /* 390px 级窄屏：底部的状态/主题钮换到第二行，避免裁切 */
  }
  .logo { font-size: 15px; gap: 6px; }
  nav { flex-direction: row; flex: 1; }
  .nav-item { padding: 7px 10px; }
  .nav-item span { display: none; }
  .bottom { flex-direction: row; align-items: center; }
  .side-stats { display: none; }
  .status-text { display: none; }
  .theme-btn { width: auto; justify-content: center; padding-left: 0; }
  /* 折叠是桌面行为，移动端横排侧栏不显示开关 */
  .collapse-btn { display: none; }
}
</style>
