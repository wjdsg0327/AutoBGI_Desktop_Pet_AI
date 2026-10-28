import type { PetDragStart } from '../bridge'

// 每次手势保留独立坐标和宿主令牌，松开后的最终位置不会被下一次按下覆盖。
type DragPoint = { x: number; y: number }
type DragSession = {
  press: DragPoint
  latest: DragPoint
  origin: PetDragStart | null
  applied: DragPoint | null
  dragged: boolean
  released: boolean
  cancelled: boolean
  cancelSent: boolean
  finished: boolean
  moveQueued: boolean
  frame: number
}

// 所有原生调用共用串行队列，新手势读取起点前必须等旧位移和位置保存完成。
export class PetDragController {
  private active: DragSession | null = null
  private sessions = new Set<DragSession>()
  private operations: Promise<void> = Promise.resolve()
  private disposed = false

  constructor(private readonly native: {
    begin: () => Promise<PetDragStart | null>
    move: (token: string, x: number, y: number) => Promise<boolean>
    end: (token: string) => Promise<boolean>
    cancel: (token: string) => Promise<boolean>
    feedback: (dragging: boolean) => void
  }) {}

  // 普通单击不读取原生窗口；异常重复按下只取消尚未松开的旧手势。
  down(x: number, y: number) {
    if (this.disposed || !Number.isFinite(x) || !Number.isFinite(y)) return
    if (this.active) {
      this.cancel(this.active)
      this.sessions.delete(this.active)
    }
    this.active = {
      press: { x, y }, latest: { x, y }, origin: null, applied: null,
      dragged: false, released: false, cancelled: false, cancelSent: false,
      finished: false, moveQueued: false, frame: 0,
    }
    this.sessions.add(this.active)
  }

  // 超过阈值才进入拖动，读取起点也排在同一队列，避免读到旧位移应用前的位置。
  move(x: number, y: number) {
    const session = this.active
    if (!session || this.disposed || session.cancelled || !Number.isFinite(x) || !Number.isFinite(y)) return
    session.latest = { x, y }
    if (!session.dragged) {
      if (Math.hypot(x - session.press.x, y - session.press.y) <= 4) return
      session.dragged = true
      this.native.feedback(true)
      void this.enqueue(session, async () => {
        const origin = await this.native.begin()
        // 先保存可用于清理的令牌，取消后的迟到 begin 仍须在同一串行操作内归还宿主手势。
        if (origin && typeof origin.token === 'string' && origin.token.length > 0) session.origin = origin
        if (this.disposed || session.cancelled) {
          await this.cleanup(session)
          return
        }
        if (!session.origin || !Number.isFinite(session.origin.x) || !Number.isFinite(session.origin.y)) {
          this.markCancelled(session)
          await this.cleanup(session)
          return
        }
      })
    }
    this.scheduleMove(session)
  }

  // 每次等待最多保留一个待发位移，回调执行时使用该手势最新的指针位置。
  private scheduleMove(session: DragSession) {
    if (this.disposed || session.cancelled || session.released || session.frame || session.moveQueued) return
    session.frame = requestAnimationFrame(() => {
      session.frame = 0
      if (this.disposed || session.cancelled || session.released || session.moveQueued) return
      session.moveQueued = true
      void this.enqueue(session, async () => {
        session.moveQueued = false
        if (session.released) return
        await this.applyLatest(session)
      })
    })
  }

  // 队列失败只结束对应手势，不重发失败调用，也不阻塞后续手势读取真实起点。
  private enqueue(session: DragSession, work: () => Promise<void>): Promise<void> {
    const operation = this.operations.then(async () => {
      if (this.disposed || session.cancelled) {
        await this.cleanup(session)
        return
      }
      try {
        await work()
      } catch {
        // 失败在当前串行项内完成令牌清理，下一手势读取起点前不会遗留旧的宿主占用。
        this.markCancelled(session)
        await this.cleanup(session)
      }
    })
    this.operations = operation
    return operation
  }

  // 起点未返回时保留最终指针；相同位置不重复发送，窗口实际限制仍由原生层负责。
  private async applyLatest(session: DragSession) {
    if (this.disposed || session.cancelled || !session.origin) return
    const target = {
      x: Math.round(session.origin.x + session.latest.x - session.press.x),
      y: Math.round(session.origin.y + session.latest.y - session.press.y),
    }
    if (!Number.isFinite(target.x) || !Number.isFinite(target.y)) {
      this.markCancelled(session)
      await this.cleanup(session)
      return
    }
    if (session.applied?.x === target.x && session.applied?.y === target.y) return
    const accepted = await this.native.move(session.origin.token, target.x, target.y)
    if (accepted !== true) this.markCancelled(session)
    if (this.disposed || session.cancelled) await this.cleanup(session)
    else session.applied = target
  }

  // 松开先冻结该手势，等待 begin 和旧 move 后应用最终帧，再且仅再保存一次位置。
  async up(x?: number, y?: number): Promise<boolean> {
    const session = this.active
    if (!session || this.disposed) return false
    this.active = null
    session.released = true
    if (x !== undefined && y !== undefined && Number.isFinite(x) && Number.isFinite(y)) session.latest = { x, y }
    if (session.frame) cancelAnimationFrame(session.frame)
    session.frame = 0
    this.native.feedback(false)
    if (!session.dragged) {
      this.sessions.delete(session)
      return false
    }
    await this.enqueue(session, async () => {
      await this.applyLatest(session)
      if (!this.disposed && !session.cancelled && session.origin) {
        session.finished = await this.native.end(session.origin.token) === true
        if (!session.finished) {
          this.markCancelled(session)
          await this.cleanup(session)
        }
      }
    })
    this.sessions.delete(session)
    return true
  }

  // 本地取消只影响对应手势，迟到失败不会结束新手势的拖动反馈。
  private markCancelled(session: DragSession) {
    session.cancelled = true
    if (session.frame) cancelAnimationFrame(session.frame)
    session.frame = 0
    if (this.active === session) this.native.feedback(false)
  }

  // 清理只在串行链内部执行且最多发送一次，令牌失效或清理异常均不自动重试。
  private async cleanup(session: DragSession) {
    if (!session.origin || session.cancelSent || session.finished) return
    session.cancelSent = true
    try { await this.native.cancel(session.origin.token) } catch { /* 宿主令牌不明时不重发或保存旧手势。 */ }
  }

  // 显式取消立即撤销待发操作，宿主清理排在在途操作之后且始终带原手势令牌。
  private cancel(session: DragSession) {
    if (session.cancelled) return
    this.markCancelled(session)
    void this.enqueue(session, async () => {})
  }

  // 隐藏、休眠或取消捕获只撤销未完成手势，迟到的起点不会再补位移或保存；串行链继续保留。
  cancelActive() {
    if (this.disposed) return
    for (const session of this.sessions) this.cancel(session)
    this.sessions.clear()
    this.active = null
    this.native.feedback(false)
  }

  // 卸载清理所有手势和动画帧，迟到的 begin/move 回包无法继续写入原生窗口。
  dispose() {
    this.disposed = true
    for (const session of this.sessions) this.cancel(session)
    this.sessions.clear()
    this.active = null
    this.native.feedback(false)
  }
}
