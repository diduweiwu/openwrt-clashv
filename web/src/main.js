import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router.js'
import './style.css'
import './glass.js'

createApp(App).use(router).mount('#app')
