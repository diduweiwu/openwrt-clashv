import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router.js'
import './style.css'
import './glass.js'

// LuCI iframe 内嵌检测的兜底：正式挂类在 index.html 防白屏内联脚本里（它最先
// 执行，theme.js 模块初始化写底色时依赖该类已就位）。这里补一份防 index.html
// 被缓存等边缘情况漏挂；已挂过则幂等。内嵌时页面底色全透明让位给 LuCI 主题
// 背景（见 style.css 的 embedded 段），卡片/侧栏底色与磨砂、极光均不受影响；
// 直接访问 :9097 时不挂类、行为不变。window.self !== window.top 跨域可比。
try {
  if (window.self !== window.top) document.documentElement.classList.add('embedded')
} catch { /* 极端环境取 window.top 抛错：按未内嵌处理 */ }

createApp(App).use(router).mount('#app')
