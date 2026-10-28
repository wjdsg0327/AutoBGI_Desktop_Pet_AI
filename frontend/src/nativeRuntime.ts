// 此类型仅描述 Wails 的本地启动桥，不是业务 DTO，也不提供通用原生调用。
type NativeHost = Window & { _wails?: { environment?: { OS?: string }; flags?: Record<string, unknown> }; chrome?: { webview?: { postMessage?: unknown } } }

export function isNative(): boolean {
  const host = window as NativeHost
  // WebView2 先提供消息桥，environment 要等 NavigationCompleted 后才注入。
  return host._wails?.environment !== undefined || typeof host.chrome?.webview?.postMessage === 'function'
}

function configurationAvailable(): boolean {
  const runtime = (window as NativeHost)._wails
  return typeof runtime?.environment?.OS === 'string' && runtime.flags !== null && typeof runtime.flags === 'object'
}

// 先等 Core 注入的正式 ready 事件，再装载带鼠标监听副作用的 npm runtime。
// 不使用顶层 await 阻塞文档加载，且超时后移除监听，不留下永久等待任务。
export function waitForRuntimeConfiguration(timeoutMs = 8000): Promise<void> {
  if (!isNative() || configurationAvailable()) return Promise.resolve()
  return new Promise<void>((resolve, reject) => {
    let timer: ReturnType<typeof setTimeout>
    const cleanup = () => { clearTimeout(timer); window.removeEventListener('wails:runtime-config-ready', ready) }
    const ready = () => { if (configurationAvailable()) { cleanup(); resolve() } }
    window.addEventListener('wails:runtime-config-ready', ready)
    timer = setTimeout(() => { cleanup(); reject(new Error('桌面运行环境初始化超时。请关闭并重新启动桌宠；系统托盘仍可用于退出。')) }, timeoutMs)
    // 注册监听后再次检查，覆盖检查与订阅之间 Core 恰好完成注入的边界。
    ready()
  })
}
