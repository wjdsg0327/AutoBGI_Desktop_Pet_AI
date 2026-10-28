import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import './style.css'
import { isNative, waitForRuntimeConfiguration } from './nativeRuntime'

// 每个窗口只维护脱敏展示状态，实际采集始终由唯一的 Go 服务负责。
async function start() {
  // Core 已注入环境和窗口 flags 后才启动 runtime，避免早期拖动读取未就绪 flags。
  if (isNative()) { await waitForRuntimeConfiguration(); await import('@wailsio/runtime') }
  createApp(App).use(createPinia()).mount('#app')
}
void start().catch(reason => {
  // 初始化失败留一个可见的纯文本出口，不静默变成假连接或浏览器演示。
  const root = document.getElementById('app')
  if (root) { root.className = 'startup-error'; root.setAttribute('role', 'alert'); root.textContent = reason instanceof Error ? reason.message : '桌宠初始化失败，请重新启动。' }
})
