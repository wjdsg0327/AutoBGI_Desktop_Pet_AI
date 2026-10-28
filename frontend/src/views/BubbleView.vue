<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '../stores/app'

// 状态摘要按字段分行，普通台词保持原样。
const store = useAppStore()
const statusRows = computed(() => store.bubble.text.split('\n').filter(Boolean).map(line => {
  const text = line.replace(/^【|】$/g, '')
  const separator = text.search(/[：:]/)
  return separator < 0 ? { label: '', value: text } : {
    label: text.slice(0, separator).replace(/^最近/, ''),
    value: text.slice(separator + 1).trim()
  }
}))
</script>

<template>
  <main class="bubble-window" :class="{ 'bubble-window--status': store.bubble.kind === 'status' }">
    <div v-if="store.bubble.kind === 'status'" class="bubble-status">
      <div v-for="(row, index) in statusRows" :key="index" class="bubble-status-row">
        <span v-if="row.label" class="bubble-status-label">{{ row.label }}</span>
        <span class="bubble-status-value">{{ row.value }}</span>
      </div>
    </div>
    <p v-else>{{ store.bubble.text }}</p>
  </main>
</template>
