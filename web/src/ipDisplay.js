// 首页「出口 IP」显示开关：与菜单模式同类的纯浏览器本地偏好
// （localStorage 持久化，不走后端设置/保存按钮）。默认显示。
import { ref, watch } from 'vue'

const KEY = 'clashv_show_ips'

export const showIps = ref(localStorage.getItem(KEY) !== '0')

watch(showIps, (v) => {
  localStorage.setItem(KEY, v ? '1' : '0')
})
