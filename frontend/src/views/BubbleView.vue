<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '../stores/app'

// 状态摘要按字段分行，普通台词保持原样。
const store = useAppStore()
const parsedRows = computed(() => store.bubble.text.split('\n').filter(Boolean).map(line => {
  const text = line.replace(/^【|】$/g, '')
  const separator = text.search(/[：:]/)
  return separator < 0 ? { label: '', value: text } : {
    label: text.slice(0, separator).replace(/^最近/, ''),
    value: text.slice(separator + 1).trim()
  }
}))

// 将服务端 Go 时长转换为毫秒，无法识别时保留未知状态。
function durationMs(value: string | undefined): number | null {
  if (!value) return null
  const text = value.trim()
  if (text === '0') return 0
  const units: Record<string, number> = { h: 3600000, m: 60000, s: 1000, ms: 1, 'µs': 0.001, 'μs': 0.001, ns: 0.000001 }
  const parts = [...text.matchAll(/(\d+(?:\.\d+)?)(ms|µs|μs|ns|h|m|s)/g)]
  if (!parts.length || parts.map(part => part[0]).join('') !== text) return null
  const total = parts.reduce((sum, part) => sum + Number(part[1]) * units[part[2]], 0)
  return Number.isFinite(total) ? total : null
}

// 进度按已运行时间和历史耗时估算，显示上限为百分之百。
const timingLabels = new Set(['时间', '时长', '已经运行', '预计'])
const statusRows = computed(() => parsedRows.value.filter(row => !timingLabels.has(row.label)))
const hasTiming = computed(() => parsedRows.value.some(row => timingLabels.has(row.label)))
const runtimeProgress = computed(() => {
  const duration = durationMs(parsedRows.value.find(row => row.label === '时长')?.value)
  const elapsed = durationMs(parsedRows.value.find(row => row.label === '已经运行')?.value)
  if (duration === null || duration <= 0 || elapsed === null) return null
  return Math.min(100, Math.round(elapsed / duration * 100))
})
</script>

<template>
  <main class="bubble-window" :class="{ 'bubble-window--status': store.bubble.kind === 'status' }">
    <div v-if="store.bubble.kind === 'status'" class="bubble-status">
      <div v-for="(row, index) in statusRows" :key="index" class="bubble-status-row">
        <span v-if="row.label" class="bubble-status-label">{{ row.label }}</span>
        <span class="bubble-status-value">{{ row.value }}</span>
      </div>
      <!-- 历史耗时不足时显示未知，避免把缺失数据误认为零进度。 -->
      <div v-if="hasTiming" class="bubble-progress">
        <div class="bubble-progress-heading">
          <span>预计进度</span>
          <span>{{ runtimeProgress !== null ? `${runtimeProgress}%` : '暂无估算' }}</span>
        </div>
        <div class="bubble-progress-track" role="progressbar" :aria-valuenow="runtimeProgress ?? undefined" aria-valuemin="0" aria-valuemax="100" :aria-valuetext="runtimeProgress !== null ? `预计进度 ${runtimeProgress}%` : '暂无估算'" aria-label="预计运行进度">
          <div v-if="runtimeProgress !== null" class="bubble-progress-fill" :style="{ width: `${runtimeProgress}%` }"></div>
        </div>
      </div>
    </div>
    <p v-else>{{ store.bubble.text }}</p>
  </main>
</template>
