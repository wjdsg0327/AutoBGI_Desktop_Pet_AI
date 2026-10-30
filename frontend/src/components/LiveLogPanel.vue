<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

// 日志只接收宿主快照，重复正文仍逐条保留，不在展示层过滤真实事件。
const props = defineProps<{
  logs: { time: string; text: string }[]
  status: string
  statusLabel: string
  fileName: string
}>()
const emit = defineEmits<{ configure: [] }>()
const viewport = ref<HTMLElement | null>(null)
const following = ref(true)
let animationFrame = 0
let lastWrittenTop = -1
let resizeObserver: ResizeObserver | undefined

// 稳定键不包含数组下标，头部淘汰后仍复用原来的行；完全相同的记录用出现次序区分。
const rows = computed(() => {
  const occurrences = new Map<string, number>()
  return props.logs.map(entry => {
    const identity = JSON.stringify([entry.time, entry.text])
    const occurrence = occurrences.get(identity) || 0
    occurrences.set(identity, occurrence + 1)
    return { ...entry, key: `${identity}:${occurrence}` }
  })
})

// 时间格式异常时保留原值，日志正文始终由模板按纯文本渲染。
function formatTime(value: string) {
  if (!value) return '--:--:--'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleTimeString('zh-CN', { hour12: false })
}

// 记录程序写入后的实际位置，避免平滑移动产生的 scroll 事件误判为用户上翻。
function writeScrollTop(top: number) {
  const element = viewport.value
  if (!element) return
  element.scrollTop = top
  lastWrittenTop = element.scrollTop
}

// 用户滚轮上翻、选择正文或拖动滚动条时立即让出控制权，不阻止浏览器原生滚动。
function pauseFollowing() {
  following.value = false
  cancelAnimationFrame(animationFrame)
  animationFrame = 0
}
function onScroll() {
  const element = viewport.value
  if (!element || Math.abs(element.scrollTop - lastWrittenTop) < 1) return
  // 用户离开程序写入的位置后即清除标记，手动回到原底部也能重新开启跟随。
  lastWrittenTop = -1
  if (element.scrollHeight - element.clientHeight - element.scrollTop <= 4) following.value = true
  else pauseFollowing()
}
function onWheel(event: WheelEvent) {
  if (event.deltaY < 0) pauseFollowing()
}
function onKeydown(event: KeyboardEvent) {
  if (['ArrowUp', 'PageUp', 'Home'].includes(event.key) || (event.key === ' ' && event.shiftKey)) pauseFollowing()
}

// 连续日志共用一段追随动画，每帧读取最新终点，避免高频更新反复重启动画。
function scrollToLatest(animate = false) {
  const element = viewport.value
  if (!element || !element.clientHeight) return
  if (!animate || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    cancelAnimationFrame(animationFrame)
    animationFrame = 0
    writeScrollTop(element.scrollHeight)
    return
  }
  if (animationFrame) return
  let previousTime = performance.now()
  const step = (now: number) => {
    animationFrame = 0
    if (!following.value || !element.isConnected) return
    const distance = element.scrollHeight - element.clientHeight - element.scrollTop
    // 末尾直接贴底，避免像素取整让动画停在几像素之外；恢复窗口后按真实耗时追上。
    if (Math.abs(distance) <= 4) { writeScrollTop(element.scrollHeight); return }
    const elapsed = now - previousTime
    previousTime = now
    writeScrollTop(element.scrollTop + distance * (1 - Math.exp(-elapsed / 65)))
    animationFrame = requestAnimationFrame(step)
  }
  animationFrame = requestAnimationFrame(step)
}
function resumeFollowing() {
  // 显式跳回最新时立即定位，取消尚未结束的原生翻页惯性，后续新日志再平滑跟随。
  following.value = true
  scrollToLatest()
}

// 广播快照可能只更新连接状态；只有日志实际改变时才滚动，满 200 条后也继续生效。
watch(rows, async (next, previous) => {
  if (next.length === previous.length && next.every((row, index) => row.key === previous[index]?.key)) return
  const element = viewport.value
  // 记录仍可见的旧行及其偏移，淘汰头部缓存时优先保持正在阅读的内容不动。
  const anchors = element && !following.value
    ? Array.from(element.querySelectorAll<HTMLElement>('.log-entry'))
      .filter(row => row.offsetTop + row.offsetHeight > element.scrollTop)
      .map(row => ({ row, offset: row.offsetTop - element.scrollTop }))
    : []
  await nextTick()
  if (!viewport.value) return
  if (!next.length) {
    following.value = true
    scrollToLatest()
  } else if (following.value) {
    scrollToLatest(previous.length > 0)
  } else if (anchors.length) {
    const anchor = anchors.find(item => viewport.value?.contains(item.row))
    writeScrollTop(anchor ? anchor.row.offsetTop - anchor.offset : 0)
  }
})

// 首次打开、窗口缩放和隐藏后恢复时校正底部；查看历史时不因尺寸变化强行跟随。
onMounted(() => {
  scrollToLatest()
  resizeObserver = new ResizeObserver(() => { if (following.value) scrollToLatest() })
  if (viewport.value) resizeObserver.observe(viewport.value)
})
onBeforeUnmount(() => {
  cancelAnimationFrame(animationFrame)
  resizeObserver?.disconnect()
})
</script>

<template>
  <!-- 卡片头尾固定，正文独立滚动；允许键盘聚焦后使用方向键及翻页键。 -->
  <div class="live-log-panel">
    <div class="log-header">
      <div class="log-source"><span class="log-dot" :class="status" /><strong>WEBSOCKET LIVE</strong><small :title="fileName">{{ fileName || '等待日志文件' }}</small></div>
      <span>{{ statusLabel }}</span>
    </div>
    <div ref="viewport" class="log-entries" tabindex="0" role="region" aria-label="实时日志内容" @scroll.passive="onScroll" @wheel.passive="onWheel" @pointerdown="pauseFollowing" @keydown="onKeydown">
      <div v-for="entry in rows" :key="entry.key" class="log-entry"><time>{{ formatTime(entry.time) }}</time><p>{{ entry.text }}</p></div>
      <div v-if="!rows.length" class="log-empty"><span>✧</span><p>{{ status === 'online' ? '连接已建立，等待新的日志。' : '连接后，实时日志会显示在这里。' }}</p><button v-if="status === 'disconnected'" class="log-link" @click="emit('configure')">前往连接设置 →</button></div>
    </div>
    <div class="log-toolbar">
      <span>最近 {{ rows.length }} 条<span class="log-limit"> / 最多 200 条</span></span>
      <span v-if="following || !rows.length" class="follow-status">{{ rows.length ? '自动跟随中' : '等待新日志' }}</span>
      <button v-else class="follow-button" @click="resumeFollowing">已暂停跟随 · 回到最新 ↓</button>
    </div>
  </div>
</template>

<style scoped>
/* 沿用现有海蓝日志卡片，剩余窗口高度全部留给正文，不再叠加固定的 540px 滚动区。 */
.live-log-panel{display:flex;flex:1;flex-direction:column;min-height:0;overflow:hidden;border:1px solid #214960;border-radius:13px;background:#142f43;color:#d9eaf0;box-shadow:0 12px 30px #1b3a5120}
.log-header{display:flex;flex:none;align-items:center;justify-content:space-between;gap:15px;padding:15px 20px;border-bottom:1px solid #ffffff18}
.log-source{display:flex;align-items:center;gap:9px;min-width:0}.log-header strong{font:10px Georgia,serif;letter-spacing:1px;white-space:nowrap}.log-header small{overflow:hidden;color:#9bb8c8;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.log-header>span{flex:none;color:#abc4d1;font-size:11px}
.log-dot{width:7px;height:7px;flex:none;border-radius:50%;background:#9aabba}.log-dot.online{background:#6fb39e;box-shadow:0 0 0 3px #6fb39e24}.log-dot.connecting,.log-dot.backoff,.log-dot.paused{background:#d5ab62}.log-dot.auth_failed,.log-dot.error{background:#d98276}
/* 锚点由组件统一维护，禁用浏览器重复补偿；长单词换行，滚动条始终预留位置。 */
.log-entries{position:relative;flex:1;min-height:0;overflow:auto;overflow-anchor:none;overscroll-behavior:contain;scrollbar-gutter:stable;scrollbar-width:thin;scrollbar-color:#537c95 #142f43;padding:10px 20px;outline-offset:-3px}
.log-entries:focus-visible{outline:2px solid #8fcce5}.log-entry{display:grid;grid-template-columns:76px minmax(0,1fr);gap:12px;padding:8px 0;border-bottom:1px solid #ffffff08;font:12px/1.85 Consolas,"Microsoft YaHei",monospace}.log-entry time{color:#8aafc3}.log-entry p{min-width:0;white-space:pre-wrap;overflow-wrap:anywhere}.log-entry:last-child{border-bottom:0}
.log-empty{display:flex;min-height:100%;flex-direction:column;align-items:center;justify-content:center;text-align:center;color:#a1bfce;font-size:12px;line-height:2}.log-empty>span{font-size:28px;color:#c5b28a}.log-link{margin-top:12px;border:0;background:none;color:#a7cadd;font-size:12px}
/* 跟随入口始终位于卡片底部，不遮挡正文；暂停时仍保留完整的原生滚动操作。 */
.log-toolbar{display:flex;flex:none;align-items:center;justify-content:space-between;gap:12px;min-height:47px;padding:8px 16px;border-top:1px solid #ffffff18;background:#102a3d;color:#9bb8c8;font-size:11px}.follow-status{color:#91cabb}.follow-button{padding:6px 10px;border:1px solid #608da4;border-radius:6px;background:#234b63;color:#deeff7;font-size:11px}.follow-button:hover{background:#2d5b75}
/* 窄窗口压缩时间列并允许头部换行，超长文件名仍可通过悬停查看。 */
@media(max-width:700px){.log-header{flex-wrap:wrap;gap:8px;padding:12px}.log-source{flex:1 1 180px}.log-entries{padding:8px 12px}.log-entry{grid-template-columns:62px minmax(0,1fr);gap:8px;font-size:11px}.log-toolbar{padding:8px 10px;gap:6px}.log-limit{display:none}}
@media(max-width:420px){.log-entry{grid-template-columns:1fr;gap:0}.log-source{flex-wrap:wrap}.log-source small{flex-basis:100%}.log-toolbar{font-size:10px}.follow-button{font-size:10px;padding:6px}}
</style>
