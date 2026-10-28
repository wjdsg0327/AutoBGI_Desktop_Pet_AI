<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { desktop, listen } from '../bridge'
import { useAppStore } from '../stores/app'
import PetCanvas from '../components/PetCanvas.vue'
import { PetDragController } from '../pet/drag'

// 点击和双击延迟去歧义；拖动由前端按屏幕指针位移移动原生窗口。
// 不使用 --wails-draggable 的系统 modal 移动循环：那种拖拽会独占全局鼠标捕获，
// 窗口繁忙时其他窗口收不到鼠标事件，整个桌面看起来像卡死（键盘仍可用）。
const store = useAppStore()
const waving = ref(false)
const triggeredAction = ref('')
const canvasReady = ref(false)
const pickupReady = ref(false)
const dragging = ref(false)
const documentVisible = ref(!document.hidden)
const hunger = ref<'normal' | 'hungry' | 'weak'>('normal')
const pickupName = ref('')
let clickTimer: ReturnType<typeof setTimeout> | undefined
let hungerTimer: ReturnType<typeof setTimeout> | undefined
let actionTimer: ReturnType<typeof setTimeout> | undefined
let triggerTimer: ReturnType<typeof setTimeout> | undefined
let triggerWaitTimer: ReturnType<typeof setTimeout> | undefined
let triggerFrame: number | undefined
let stopTriggers: (() => void) | undefined
let lastDispatchAttempt = 0
let triggerAttempt = 0
let moved = false
let disposed = false
let hadVisibleWindow = false
let activePointer: number | null = null
let pointerTarget: HTMLElement | null = null
let documentVisibilityReported: boolean | undefined
let documentVisibilitySending = false

// 可见与交互分别判定；穿透仅关闭鼠标交互，保留角色当前动画，隐藏或休眠才暂停动画。
const sleeping = computed(() => store.presentation.action === 'sleep')
// 由宿主统一判定饿晕，避免前端提前中断而宿主尚未将动作重新排队。
const fainted = computed(() => store.presentation.action === 'fainted')
const available = computed(() => store.presentation.visible && store.state?.preferences.pet.visible !== false && documentVisible.value)
const mouseAvailable = computed(() => available.value && store.state?.preferences.pet.clickThrough !== true)
const animated = computed(() => available.value && !sleeping.value)
const interactive = computed(() => mouseAvailable.value && !sleeping.value)
const action = computed(() => fainted.value ? 'faint' : triggeredAction.value === 'eat' ? 'pickup' : triggeredAction.value || (dragging.value ? 'dragging' : waving.value ? 'wave' : hunger.value !== 'normal' ? hunger.value : 'idle'))
const canWave = computed(() => interactive.value && !fainted.value && !dragging.value && !triggeredAction.value)
// 初始状态尚未读到时保留待确认动作，避免窗口尚未就绪就消耗队列时间。
const visibleWindow = computed(() => animated.value && store.state !== null)
const canPresentTrigger = computed(() => canvasReady.value && visibleWindow.value && !fainted.value)

// 仅以在线日志流的最后活动时间计算饥饿，连接建立或任何新日志都会重置起点。
type ActivityState = { logStream?: { status?: string; lastActivityAt?: string } }
function updateHunger() {
  clearTimeout(hungerTimer)
  hungerTimer = undefined
  const stream = (store.state as ActivityState | null)?.logStream
  const last = Date.parse(stream?.lastActivityAt || '')
  if (stream?.status !== 'online' || !Number.isFinite(last)) {
    hunger.value = 'normal'
    return
  }
  const elapsed = Math.max(0, Date.now() - last)
  hunger.value = elapsed >= 180000 ? 'weak' : elapsed >= 60000 ? 'hungry' : 'normal'
  const next = elapsed < 60000 ? 60000 : elapsed < 180000 ? 180000 : 0
  if (next) hungerTimer = setTimeout(updateHunger, Math.max(1, next - elapsed))
}
watch(() => {
  const stream = (store.state as ActivityState | null)?.logStream
  return [stream?.status, stream?.lastActivityAt]
}, updateHunger, { immediate: true })

// 每个触发单独保存身份和阶段，迟到的确认或绘制回调不能清理后续动作。
type TriggerEvent = { id: number; generation: number; attempt: number; action: string; keyword?: string; pending?: boolean; expiresAt?: string; confirmUntil?: string }
type TriggerState = { id: number; generation: number; attempt: number; action: string; keyword: string; phase: 'waiting' | 'painting' | 'playing'; confirmUntil: number }
let currentTrigger: TriggerState | null = null

// 饿晕立即中断前端展示；宿主重排后使用新的派发编号完整重播。
watch(fainted, value => {
  if (value && currentTrigger) {
    clearTrigger(currentTrigger)
  }
}, { flush: 'sync' })

// 关闭拾取通知后，马上移除当前蛋糕画面。
watch(() => (store.state as { preferences?: { notifications?: { pickupEnabled?: boolean } } } | null)?.preferences?.notifications?.pickupEnabled, enabled => {
  if (enabled === false) {
    if (currentTrigger?.action === 'eat') clearTrigger(currentTrigger)
  }
}, { flush: 'sync' })

// 清除动作本身与期限，迟到回调无法把已读后的工作态或新错误改回挥手。
function stopWave() {
  clearTimeout(actionTimer)
  actionTimer = undefined
  waving.value = false
}

// 单击去歧义也必须在隐藏和卸载时取消，不能恢复后才发出旧的摘要请求。
function stopInteraction() {
  clearTimeout(clickTimer)
  clickTimer = undefined
  stopWave()
}

const drag = new PetDragController({
  begin: async () => (await desktop()).BeginPetDrag(),
  move: async (token, x, y) => (await desktop()).DragPetTo(token, x, y),
  end: async token => (await desktop()).PetDragged(token),
  cancel: async token => (await desktop()).CancelPetDrag(token),
  feedback: value => {
    dragging.value = value
    if (value) { moved = true; stopInteraction() }
  },
})

// 开始新规则动作时取消点击挥手；隐藏时结束当前手势和待发点击。
watch(canWave, allowed => { if (!allowed) stopWave() }, { flush: 'sync' })
watch(interactive, active => {
  if (!active) { moved = true; stopInteraction(); endPointer(true) }
}, { flush: 'sync' })

// 同时只接受一个主指针，第二触点不能覆盖手势起点或提前松开正在拖动的角色。
function pointerDown(event: PointerEvent) {
  if (event.button !== 0 || event.isPrimary === false || activePointer !== null || !interactive.value) return
  moved = false
  activePointer = event.pointerId
  drag.down(event.screenX, event.screenY)
  // 元素级指针捕获保留移出角色后的松开事件，不进入 Windows 全局模态移动循环。
  const target = event.currentTarget as HTMLElement
  pointerTarget = target
  target.setPointerCapture(event.pointerId)
}

function pointerMove(event: PointerEvent) {
  if (event.pointerId === activePointer && interactive.value && (event.buttons & 1)) drag.move(event.screenX, event.screenY)
}

// 先清主指针再释放捕获，随后到达的 lostpointercapture 不会重复结束同一手势。
function endPointer(cancelled = false, x?: number, y?: number) {
  const pointerId = activePointer
  const target = pointerTarget
  activePointer = null
  pointerTarget = null
  if (pointerId !== null && target?.hasPointerCapture(pointerId)) target.releasePointerCapture(pointerId)
  if (cancelled) drag.cancelActive()
  else void drag.up(x, y)
}

function pointerUp(event: PointerEvent) { if (event.pointerId === activePointer) endPointer(false, event.screenX, event.screenY) }

// 取消和意外失去捕获只做收尾，不将被中断的手势转换成点击或挥手。
function pointerCancel(event: PointerEvent) {
  if (event.pointerId !== activePointer) return
  moved = true
  stopInteraction()
  endPointer(true)
}

function click() {
  if (moved) { moved = false; return }
  if (!interactive.value || disposed) return
  clearTimeout(clickTimer)
  clickTimer = setTimeout(() => {
    clickTimer = undefined
    if (!interactive.value || disposed) return
    if (canWave.value) {
      stopWave()
      waving.value = true
      actionTimer = setTimeout(() => { actionTimer = undefined; waving.value = false }, 1200)
    }
    void store.run(async () => (await desktop()).PetClicked())
  }, 260)
}

// 双击与菜单只请求宿主白名单入口，既不修改已读状态，也不生成业务完成动画。
function doubleClick() { stopInteraction(); if (mouseAvailable.value && !disposed) void store.run(async () => (await desktop()).ShowPanel()) }
function contextMenu() { stopInteraction(); if (mouseAvailable.value && !disposed) void store.run(async () => (await desktop()).ShowContextMenu()) }
// 可见性上报串行发送，快速隐藏和恢复时旧请求不能覆盖最后一次状态。
async function reportDocumentVisibility() {
  if (!store.native || documentVisibilitySending || disposed) return
  documentVisibilitySending = true
  try {
    while (!disposed && documentVisibilityReported !== documentVisible.value) {
      const visible = documentVisible.value
      await (await desktop()).SetPetDocumentVisible(visible)
      documentVisibilityReported = visible
    }
  } catch { /* 恢复可见或下次变化时重新尝试上报。 */ }
  finally { documentVisibilitySending = false }
}
function visibilityChanged() { documentVisible.value = !document.hidden; updateHunger(); void reportDocumentVisibility() }

// 取消尚未呈现的帧等待；版本让已排队的 nextTick 和 RAF 同样失效。
function cancelTriggerFrame() {
  triggerAttempt++
  if (triggerFrame !== undefined) cancelAnimationFrame(triggerFrame)
  triggerFrame = undefined
}

// 清理只作用于当前身份，过期的确认回包不能结束新动作。
function clearTrigger(expected: TriggerState) {
  if (currentTrigger !== expected) return
  cancelTriggerFrame()
  clearTimeout(triggerTimer)
  clearTimeout(triggerWaitTimer)
  triggerTimer = undefined
  triggerWaitTimer = undefined
  currentTrigger = null
  triggeredAction.value = ''
  pickupName.value = ''
}

// 首帧前隐藏时退回等待；恢复可见后仍能播放尚未超时的动作。
function suspendPendingTrigger() {
  const trigger = currentTrigger
  if (!trigger || trigger.phase !== 'painting') return
  cancelTriggerFrame()
  trigger.phase = 'waiting'
  triggeredAction.value = ''
}

// 素材就绪后先设置动作，让渲染器在 Vue 更新中安排该动作的绘制。
function startPendingTrigger() {
  const trigger = currentTrigger
  if (disposed || !trigger || trigger.phase !== 'waiting' || !canPresentTrigger.value || trigger.action === 'eat' && !pickupReady.value) return
  if (Date.now() >= trigger.confirmUntil) { clearTrigger(trigger); return }
  if (store.state?.connection.generation !== trigger.generation) return
  trigger.phase = 'painting'
  const attempt = ++triggerAttempt
  triggeredAction.value = trigger.action
  pickupName.value = trigger.action === 'eat' ? trigger.keyword : ''
  const current = () => !disposed && currentTrigger === trigger && trigger.phase === 'painting' &&
    triggerAttempt === attempt && canPresentTrigger.value &&
    store.state?.connection.generation === trigger.generation && Date.now() < trigger.confirmUntil
  // 等待期间到期时直接撤销动作，不能让已经失效的首帧停留在画面上。
  const stillCurrent = () => {
    if (current()) return true
    if (currentTrigger === trigger && Date.now() >= trigger.confirmUntil) clearTrigger(trigger)
    return false
  }
  void nextTick().then(() => {
    if (!stillCurrent()) return
    // 首次 RAF 等待画布绘制，第二次 RAF 确保上一帧已有机会呈现。
    triggerFrame = requestAnimationFrame(() => {
      triggerFrame = undefined
      if (!stillCurrent()) return
      triggerFrame = requestAnimationFrame(() => {
        triggerFrame = undefined
        if (!stillCurrent()) return
        trigger.phase = 'playing'
        clearTimeout(triggerWaitTimer)
        triggerWaitTimer = undefined
        // 首帧呈现后，拾取保持一点五秒，关键词动作保持五秒。
        triggerTimer = setTimeout(() => clearTrigger(trigger), trigger.action === 'eat' ? 1500 : 5000)
        void (async () => {
          try {
            const accepted = await (await desktop()).StartKeywordTrigger(trigger.id, trigger.generation, trigger.attempt)
            if (!accepted) clearTrigger(trigger)
          } catch {
            clearTrigger(trigger)
          }
        })()
      })
    })
  })
}

// 已显示过的窗口被隐藏或连接代次变化后清理当前动作；初始加载则继续等待。
watch([visibleWindow, canvasReady, pickupReady, fainted, () => store.state?.connection.generation], ([visible], previous) => {
  if (visibleWindow.value) hadVisibleWindow = true
  // 显示恢复后补取宿主队首，避免先于窗口可见的重播事件丢失。
  if (visible && previous?.[0] === false && store.native) {
    void (async () => {
      try {
        const current = await (await desktop()).GetPresentation()
        if (!disposed && visibleWindow.value && current?.keywordTrigger) playTrigger(current.keywordTrigger as TriggerEvent)
      } catch { /* 窗口关闭时不恢复已失效的动作。 */ }
    })()
  }
  const trigger = currentTrigger
  if (!trigger) return
  const generation = store.state?.connection.generation
  if (hadVisibleWindow && !visibleWindow.value || generation !== undefined && generation > trigger.generation) {
    clearTrigger(trigger)
  } else if (trigger.phase === 'playing') {
    return
  } else if (!canPresentTrigger.value || generation !== trigger.generation) {
    suspendPendingTrigger()
  } else {
    startPendingTrigger()
  }
}, { immediate: true, flush: 'sync' })

// 订阅事件和补取快照按派发序号去重；动作重播使用新序号，旧快照不能重新点亮动作。
function playTrigger(value: TriggerEvent) {
  if (disposed || !value || !Number.isSafeInteger(value.id) || value.id <= 0 ||
      !Number.isSafeInteger(value.generation) || value.generation < 0 ||
      !Number.isSafeInteger(value.attempt) || value.attempt <= 0 ||
      !['idle', 'happy', 'wave', 'surprised', 'sad', 'eat'].includes(value.action) ||
      value.action === 'eat' && (typeof value.keyword !== 'string' || !value.keyword.trim())) return
  if (value.action === 'eat' && (store.state as { preferences?: { notifications?: { pickupEnabled?: boolean } } } | null)?.preferences?.notifications?.pickupEnabled === false) return
  const generation = store.state?.connection.generation
  if (generation !== undefined && generation > value.generation) return
  if (hadVisibleWindow && !visibleWindow.value) return
  const remaining = value.pending === true ? 0 : Date.parse(value.expiresAt || '') - Date.now()
  if (value.pending !== true && (!Number.isFinite(remaining) || remaining <= 0)) return
  const confirmUntil = value.pending === true && value.confirmUntil !== undefined ? Date.parse(value.confirmUntil) : Infinity
  if (value.pending === true && ((!Number.isFinite(confirmUntil) && confirmUntil !== Infinity) || confirmUntil <= Date.now())) return
  const same = currentTrigger?.id === value.id && currentTrigger.generation === value.generation && currentTrigger.attempt === value.attempt
  // 较新的已启动快照可替换尚未确认的同一动作，其他重复或迟到消息不重置期限。
  if (value.attempt < lastDispatchAttempt || value.attempt === lastDispatchAttempt &&
      (!same || value.pending === true || currentTrigger?.phase === 'playing')) return
  if (currentTrigger) clearTrigger(currentTrigger)
  lastDispatchAttempt = value.attempt
  stopWave()
  const trigger: TriggerState = {
    id: value.id, generation: value.generation, attempt: value.attempt, action: value.action, keyword: value.keyword || '',
    phase: value.pending === true ? 'waiting' : 'playing', confirmUntil,
  }
  currentTrigger = trigger
  if (trigger.phase === 'waiting') {
    if (Number.isFinite(confirmUntil)) triggerWaitTimer = setTimeout(() => clearTrigger(trigger), confirmUntil - Date.now())
    startPendingTrigger()
  }
  else {
    triggeredAction.value = trigger.action
    pickupName.value = trigger.action === 'eat' ? trigger.keyword : ''
    triggerTimer = setTimeout(() => clearTrigger(trigger), remaining)
  }
}

onMounted(async () => {
  visibilityChanged()
  document.addEventListener('visibilitychange', visibilityChanged)
  const stop = await listen<TriggerEvent>('pet:trigger', playTrigger)
  if (disposed) stop()
  else {
    stopTriggers = stop
    // 自动连接可能早于窗口加载，订阅后补取仍在展示的动作并按编号去重。
    if (store.native) {
      try {
        const current = await (await desktop()).GetPresentation()
        if (!disposed && current?.keywordTrigger) playTrigger(current.keywordTrigger as TriggerEvent)
      } catch { /* 宿主关闭期间无需恢复过期动作。 */ }
    }
  }
})
onBeforeUnmount(() => {
  disposed = true
  stopTriggers?.()
  if (currentTrigger) clearTrigger(currentTrigger)
  else cancelTriggerFrame()
  stopInteraction()
  clearTimeout(hungerTimer)
  endPointer(true)
  drag.dispose()
  document.removeEventListener('visibilitychange', visibilityChanged)
})
</script>

<template>
  <main class="pet-window" :class="`pet-action-${action}`" :aria-label="`芙宁娜桌宠，${store.statusLabel}`" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerCancel" @lostpointercapture="pointerCancel" @click="click" @dblclick="doubleClick" @contextmenu.prevent="contextMenu"><PetCanvas :action="action" :pickup-name="pickupName" :visible="animated" @ready="canvasReady = true" @pickup-ready="pickupReady = true" /><span class="pet-status-label"><i class="status-dot" :class="store.status" />{{ store.status === 'online' ? 'ABGI已连接' : store.status === 'connecting' ? '正在连接' : '芙宁娜' }}</span></main>
</template>
