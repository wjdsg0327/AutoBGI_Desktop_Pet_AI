// 素材元数据仅描述受限图片帧，不执行包内脚本或加载远程资源。
type Action = { frames: number[]; fps: number; loop: boolean }
type Manifest = { schemaVersion: number; atlas: string; frameWidth: number; frameHeight: number; columns: number; rows: number; actions: Record<string, Action>; fallbackAction?: string; scale?: number; anchor?: { x: number; y: number } }

// 实际读取流也限制为 64 KiB，不信任可缺失或伪造的 Content-Length。
async function readManifest(response: Response, signal: AbortSignal): Promise<Manifest> {
  const limit = 64 * 1024
  if (Number(response.headers.get('Content-Length')) > limit) {
    void response.body?.cancel().catch(() => {})
    throw new Error('角色描述文件超出限制')
  }
  if (!response.body) throw new Error('角色描述文件为空')
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  try {
    for (;;) {
      if (signal.aborted) throw new Error('角色加载已取消')
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > limit) throw new Error('角色描述文件超出限制')
      chunks.push(value)
    }
    const data = new Uint8Array(size)
    let offset = 0
    for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.byteLength }
    return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(data)) as Manifest
  } catch (reason) {
    void reader.cancel().catch(() => {})
    throw reason
  } finally {
    reader.releaseLock()
  }
}

export class SpriteRenderer {
  private image = new Image()
  private manifest: Manifest | null = null
  private action = 'idle'
  private started = 0
  private frame = -1
  private raf = 0
  private timer: ReturnType<typeof setTimeout> | null = null
  private scheduleVersion = 0
  private visible = true
  private animated = true
  private disposed = false
  private loadVersion = 0
  private loadAbort: AbortController | null = null
  private loadingImage: HTMLImageElement | null = null
  private readonly visibilityHandler = () => { if (!document.hidden) this.frame = -1; this.schedule() }
  private readonly resizeHandler = () => { this.frame = -1; this.schedule() }

  constructor(private readonly canvas: HTMLCanvasElement) {
    document.addEventListener('visibilitychange', this.visibilityHandler)
    window.addEventListener('resize', this.resizeHandler)
  }

  // 由实际成功提交的图集决定可显示状态，组件失败回退不能掩盖仍然有效的旧图。
  get hasImage() { return !this.disposed && this.manifest !== null }

  // 在绘制前验证尺寸和帧索引，避免错误元数据导致无界内存与异常取帧。
  async load(path: string) {
    if (this.disposed) return false
    const version = ++this.loadVersion
    this.loadAbort?.abort()
    if (this.loadingImage) this.loadingImage.src = ''
    const abort = new AbortController()
    this.loadAbort = abort
    let image: HTMLImageElement | null = null
    try {
      // 皮肤与图集只从同源静态资源读取，重定向也不能借道加载远程描述文件。
      const source = new URL(path, location.href)
      const current = new URL(location.href)
      if (source.origin !== current.origin || source.protocol !== current.protocol || source.host !== current.host || source.username || source.password || source.search || source.hash) throw new Error('角色资源必须来自本地同源地址')
      const response = await fetch(source.href, { signal: abort.signal, redirect: 'error', credentials: 'omit' })
      // 快速换肤和窗口卸载都使旧加载失效，旧图在新素材成功前继续显示。
      if (this.disposed || version !== this.loadVersion) return false
      if (!response.ok) throw new Error('角色描述文件读取失败')
      const manifest = await readManifest(response, abort.signal)
      if (this.disposed || version !== this.loadVersion) return false
      if (!manifest || typeof manifest !== 'object' || manifest.schemaVersion !== 1 || !manifest.actions || typeof manifest.actions !== 'object' || Array.isArray(manifest.actions) || !Object.hasOwn(manifest.actions, 'idle') || typeof manifest.atlas !== 'string' || !/^[\w.-]+\.(png|webp)$/i.test(manifest.atlas)) throw new Error('角色协议不受支持')
      const { frameWidth, frameHeight, columns, rows } = manifest
      if (![frameWidth, frameHeight, columns, rows].every(value => Number.isInteger(value) && value > 0) || frameWidth * columns > 8192 || frameHeight * rows > 8192 || frameWidth * columns * frameHeight * rows > 8_000_000) throw new Error('角色图片超出限制')
      if (Object.keys(manifest.actions).length > 32) throw new Error('角色动作数量超出限制')
      for (const [name, action] of Object.entries(manifest.actions)) {
        if (!/^[a-z][a-z0-9_-]{0,31}$/.test(name) || !action || !Array.isArray(action.frames) || !action.frames.length || action.frames.length > 512 || typeof action.loop !== 'boolean' || !Number.isFinite(action.fps) || action.fps < 1 || action.fps > 30 || action.frames.some(frame => !Number.isInteger(frame) || frame < 0 || frame >= columns * rows)) throw new Error('角色动作不合法')
      }
      if (!manifest.actions.idle.loop || manifest.fallbackAction && manifest.fallbackAction !== 'idle') throw new Error('角色需要有效的循环待机动作')
      const scale = manifest.scale ?? 1
      const anchor = manifest.anchor ?? { x: 0.5, y: 1 }
      if (!Number.isFinite(scale) || scale < 0.25 || scale > 2 || !anchor || ![anchor.x, anchor.y].every(value => Number.isFinite(value) && value >= 0 && value <= 1)) throw new Error('角色缩放或锚点不合法')
      image = new Image()
      this.loadingImage = image
      image.src = new URL(manifest.atlas, source).href
      await image.decode()
      if (this.disposed || version !== this.loadVersion) return false
      if (image.naturalWidth !== frameWidth * columns || image.naturalHeight !== frameHeight * rows) throw new Error('角色图集与元数据不匹配')
      const previous = this.image
      this.image = image
      this.manifest = manifest
      this.started = performance.now()
      this.frame = -1
      previous.src = ''
      this.schedule()
      return true
    } catch (reason) {
      if (this.disposed || version !== this.loadVersion) return false
      throw reason
    } finally {
      if (image && image !== this.image) image.src = ''
      if (version === this.loadVersion) {
        this.loadAbort = null
        this.loadingImage = null
      }
    }
  }

  setAction(action: string) { if (this.action !== action) { this.action = action; this.started = performance.now(); this.frame = -1; this.schedule() } }
  setVisible(value: boolean) { if (this.visible !== value) { this.visible = value; if (value) this.frame = -1; this.schedule() } }

  // 静态预览停在当前动作首帧，切回桌宠模式时从头恢复动画。
  setAnimated(value: boolean) {
    if (this.animated === value) return
    this.animated = value
    this.started = performance.now()
    this.frame = -1
    this.schedule()
  }

  // 所有失效入口共用取消逻辑，迟到回调不能覆盖新动作或卸载后的调度。
  private cancelScheduled() {
    this.scheduleVersion++
    cancelAnimationFrame(this.raf)
    this.raf = 0
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = null
  }

  // 先等下一素材帧的期限，再请求绘制；低帧率动作不再逐显示帧唤醒脚本。
  private schedule(delay = 0) {
    this.cancelScheduled()
    if (this.disposed || !this.visible || document.hidden || !this.manifest || !this.animated && this.frame >= 0) return
    const version = this.scheduleVersion
    const paint = () => {
      if (version !== this.scheduleVersion || this.disposed || !this.visible || document.hidden) return
      this.timer = null
      this.raf = requestAnimationFrame(time => { if (version === this.scheduleVersion) this.draw(time) })
    }
    if (delay > 0) this.timer = setTimeout(paint, Math.max(1, Math.ceil(delay)))
    else paint()
  }

  private draw(time: number) {
    this.raf = 0
    const manifest = this.manifest
    if (!manifest || this.disposed || !this.visible || document.hidden) return
    // 只读取明确存在的动作，toString 等继承属性也必须回到经过校验的待机动作。
    const action = Object.hasOwn(manifest.actions, this.action) ? manifest.actions[this.action]! : manifest.actions.idle!
    const elapsedFrame = this.animated ? Math.floor(Math.max(0, time - this.started) * action.fps / 1000) : 0
    // 非循环动作结束后自动回落，避免短时 waving 或 success 永远停在末帧。
    if (this.animated && !action.loop && elapsedFrame >= action.frames.length) { this.action = manifest.fallbackAction || 'idle'; this.started = time; this.frame = -1; this.schedule(); return }
    const frame = action.frames[action.loop ? elapsedFrame % action.frames.length : Math.min(elapsedFrame, action.frames.length - 1)]!
    if (this.frame !== frame) {
      this.frame = frame
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      const width = Math.max(1, Math.round(this.canvas.clientWidth * dpr))
      const height = Math.max(1, Math.round(this.canvas.clientHeight * dpr))
      if (this.canvas.width !== width || this.canvas.height !== height) { this.canvas.width = width; this.canvas.height = height }
      const context = this.canvas.getContext('2d')
      if (context) {
        context.clearRect(0, 0, width, height)
        const ratio = Math.min(width / manifest.frameWidth, height / manifest.frameHeight)
        const w = manifest.frameWidth * ratio * (manifest.scale ?? 1)
        const h = manifest.frameHeight * ratio * (manifest.scale ?? 1)
        const anchor = manifest.anchor ?? { x: 0.5, y: 1 }
        context.drawImage(this.image, frame % manifest.columns * manifest.frameWidth, Math.floor(frame / manifest.columns) * manifest.frameHeight, manifest.frameWidth, manifest.frameHeight, (width - w) * anchor.x, (height - h) * anchor.y, w, h)
      }
    }
    // 期限锚定动作起点而非上次回调，晚帧按真实经过时间跳帧，不逐次累积漂移。
    if (this.animated) this.schedule(Math.max(1, this.started + (elapsedFrame + 1) * 1000 / action.fps - performance.now()))
  }

  // 卸载时注销监听与动画循环，不留下后台绘制任务。
  destroy() { this.disposed = true; this.loadVersion++; this.loadAbort?.abort(); if (this.loadingImage) this.loadingImage.src = ''; this.cancelScheduled(); document.removeEventListener('visibilitychange', this.visibilityHandler); window.removeEventListener('resize', this.resizeHandler); this.image.src = '' }
}
