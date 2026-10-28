<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 确认卡只呈现 Go 冻结的目标，真正的单次消费和前置条件校验仍由 Go 与服务端负责。
const store = useAppStore()
const now = ref(Date.now())
const heading = ref<HTMLElement>()
const confirmation = computed(() => store.state?.preparedCommand)
const secondsLeft = computed(() => Math.max(0, Math.ceil((Date.parse(confirmation.value?.expiresAt || '') - now.value) / 1000)) || 0)
const contextMatches = computed(() => confirmation.value && confirmation.value.profileId === store.state?.activeProfileId
  && confirmation.value.instanceId === store.snapshot?.serverInstanceId
  && confirmation.value.expectedStateVersion === store.snapshot?.stateVersion && !store.snapshot?.stale)
const canConfirm = computed(() => store.canControl && contextMatches.value && secondsLeft.value > 0)
const history = computed(() => [...(store.state?.commands || [])]
  .filter(item => item.profileId === store.state?.activeProfileId && item.instanceId === store.snapshot?.serverInstanceId)
  .sort((a, b) => (b.updatedAt || b.acceptedAt).localeCompare(a.updatedAt || a.acceptedAt)).slice(0, 20))
let timer: ReturnType<typeof setInterval> | undefined
let mounted = false
let clockVersion = 0

// 换卡、隐藏和卸载均废弃旧计时回调，已经排队的回调也不能更新新卡。
function stopClock() {
  clearInterval(timer)
  timer = undefined
  clockVersion++
}

// 只为可见页面中的有效确认计时；恢复时先重算，休眠期间过期的确认不会重新启用。
function syncClock() {
  stopClock()
  now.value = Date.now()
  if (!mounted || !store.presentation.panelVisible || document.hidden || store.presentation.action === 'sleep') return
  const current = confirmation.value
  const expiresAt = Date.parse(current?.expiresAt || '')
  if (!current?.token || !Number.isFinite(expiresAt) || expiresAt <= now.value) return
  const version = clockVersion
  timer = setInterval(() => {
    if (!mounted || version !== clockVersion) return
    now.value = Date.now()
    if (!store.presentation.panelVisible || document.hidden || store.presentation.action === 'sleep' || now.value >= expiresAt) stopClock()
  }, 500)
}

// 页面显隐和系统睡眠独立于桌宠偏好，隐藏角色时仍可在控制台正常确认。
watch([() => confirmation.value?.token, () => confirmation.value?.expiresAt, () => store.presentation.panelVisible, () => store.presentation.action === 'sleep'], syncClock, { flush: 'sync' })
onMounted(() => {
  mounted = true
  document.addEventListener('visibilitychange', syncClock)
  syncClock()
})
onBeforeUnmount(() => {
  mounted = false
  stopClock()
  document.removeEventListener('visibilitychange', syncClock)
})

// 焦点等待期间可能换卡或隐藏，只有仍匹配的可见确认才能继续引导用户。
watch(() => confirmation.value?.token, async token => {
  await nextTick()
  if (mounted && token && confirmation.value?.token === token && store.presentation.panelVisible && !document.hidden && store.presentation.action !== 'sleep') heading.value?.focus()
})

// 启动动作回执与业务任务终态分开命名，未知结果只提供查询，避免重复启动。
function receiptLabel(status: string) {
  return ({ sending: '正在提交请求', accepted: '请求已接收', executing: '正在尝试启动', succeeded: '启动动作已提交', failed: '启动未成功', rejected: '请求被拒绝', unknown: '执行结果未知' })[status] || '回执状态未知'
}
function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '时间未知' : date.toLocaleString('zh-CN', { hour12: false })
}
</script>

<template>
  <!-- 确认信息始终留在当前目标的任务页，过期或上下文变化后按钮立即失效。 -->
  <section v-if="confirmation" class="card command-confirmation" aria-labelledby="command-title">
    <div class="card-heading"><h3 id="command-title" ref="heading" tabindex="-1">确认启动这一条龙</h3><span class="subtle-badge">{{ secondsLeft ? `${secondsLeft} 秒内有效` : '确认已过期' }}</span></div>
    <dl class="detail-list"><div><dt>目标连接</dt><dd>{{ confirmation.profileName }}</dd></div><div><dt>服务地址</dt><dd>{{ confirmation.baseUrl }}</dd></div><div><dt>实例标识</dt><dd class="identifier">{{ confirmation.instanceId }}</dd></div><div><dt>执行范围</dt><dd>当前 AutoBGI 实例 · 游戏账号由该实例配置决定</dd></div><div><dt>一条龙</dt><dd>{{ confirmation.presetName }}</dd></div><div><dt>操作影响</dt><dd>{{ confirmation.impact }}</dd></div></dl>
    <p class="command-help">将向该实例发送一次启动请求。启动动作成功后，任务的实际执行结果仍需查看 AutoBGI。</p>
    <p v-if="!canConfirm" class="warning-text command-help">{{ !secondsLeft ? '确认已过期，请取消后重新选择配置。' : '当前连接或执行条件已经变化，请取消后重新准备。' }}</p>
    <div class="command-actions"><button class="button button-dark" :disabled="!canConfirm || store.busy" @click="store.confirmCommand(confirmation.token)"><AppIcon name="check" :size="16" />确认启动</button><button class="button button-light" :disabled="store.busy" @click="store.cancelPreparedCommand()">取消</button></div>
  </section>

  <!-- 历史回执按实例隔离；断网和未知结果不会出现重新执行按钮。 -->
  <section v-if="history.length" class="card command-history" aria-label="启动回执">
    <div class="card-heading"><h3>最近启动回执</h3><span class="subtle-badge">保留执行证据</span></div>
    <article v-for="receipt in history" :key="receipt.commandId" :class="{ uncertain: receipt.status === 'unknown' }">
      <div class="receipt-heading"><strong>{{ receipt.presetName || '一条龙启动' }}</strong><span class="receipt-status" :class="receipt.status">{{ receiptLabel(receipt.status) }}</span></div>
      <p>{{ receipt.message || (receipt.status === 'unknown' ? '请求可能已被执行，请查询同一命令回执，并检查 AutoBGI。' : '回执只描述启动动作，不代表一条龙任务已完成。') }}</p>
      <div class="receipt-footer"><small>{{ formatTime(receipt.updatedAt || receipt.acceptedAt) }}<code>{{ receipt.commandId }}</code></small><button class="text-button" :disabled="store.busy || store.status !== 'online'" @click="store.getCommand(receipt.commandId)"><AppIcon name="refresh" :size="13" />查询回执</button></div>
    </article>
  </section>
</template>
