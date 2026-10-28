import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 生产资源保持相对路径，确保三个本地窗口共用同一个打包入口。
export default defineConfig({
  plugins: [vue()],
  base: './',
  server: { host: '127.0.0.1', port: 9245, strictPort: true },
  build: { target: 'es2022', sourcemap: false },
})
