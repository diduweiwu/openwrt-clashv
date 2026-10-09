import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    // 监听 0.0.0.0：vite 6 默认只绑 IPv6 回环（[::1]），127.0.0.1 访问不了；
    // 开放后同一局域网的手机也能直连 5173 测移动端
    host: true,
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:9097',
    },
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 1024,
  },
})
