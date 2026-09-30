import { createRouter, createWebHistory } from 'vue-router'
import HomeView from './views/HomeView.vue'
import ProxiesView from './views/ProxiesView.vue'
import SubscriptionsView from './views/SubscriptionsView.vue'
import LogsView from './views/LogsView.vue'
import SettingsView from './views/SettingsView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/proxies', name: 'proxies', component: ProxiesView },
    { path: '/profiles', name: 'profiles', component: SubscriptionsView },
    { path: '/logs', name: 'logs', component: LogsView },
    { path: '/settings', name: 'settings', component: SettingsView },
  ],
})
