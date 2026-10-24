// 菜单布局模式管理：side=左侧竖向菜单 / top=顶部横向菜单，全局单例。
// 与主题、磨砂同类的纯浏览器本地偏好（localStorage 持久化，不走后端设置/保存按钮）。
// 仅桌面端（>760px）生效；移动端始终为顶部横向菜单（Sidebar.vue 的移动端媒体查询不受此值影响）。
import { ref, watch } from 'vue'

const KEY = 'clashv_menu_mode'

// 默认 side：保持历史版本左侧竖向菜单不变，老用户升级无感知
export const menuMode = ref(localStorage.getItem(KEY) === 'top' ? 'top' : 'side')

watch(menuMode, (v) => {
  localStorage.setItem(KEY, v)
})
