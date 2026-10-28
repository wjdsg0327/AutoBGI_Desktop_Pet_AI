<script setup lang="ts">
// 图标使用项目内自绘线条，不加载外部图标字体或可执行 SVG 内容。
defineProps<{ name: string; size?: number }>()
const paths: Record<string, string> = {
  grid: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  link: 'M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2 M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2',
  message: 'M21 11a9 9 0 0 1-9 9H4l-3 2V11a10 10 0 0 1 20 0Z M7 10h10 M7 14h6',
  list: 'M9 5h12 M9 12h12 M9 19h12 M3 5h1 M3 12h1 M3 19h1',
  settings: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z M10 2h4l1 3 3 1 3-1 2 4-2 2v3l2 2-2 4-3-1-3 1-1 3h-4l-1-3-3-1-3 1-2-4 2-2v-3L1 9l2-4 3 1 3-1Z',
  arrow: 'M5 12h14 M13 6l6 6-6 6',
  refresh: 'M20 7V2l-3 3a8 8 0 1 0 3 9 M20 7h-5',
  external: 'M14 3h7v7 M21 3l-9 9 M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5',
  plus: 'M12 5v14 M5 12h14',
  check: 'M5 12l4 4L20 5',
  close: 'M6 6l12 12 M18 6 6 18',
  shield: 'M12 2 3 6v6c0 5 9 10 9 10s9-5 9-10V6Z M8 12l3 3 5-6',
  clock: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18 M12 7v6l4 2',
  sparkle: 'M12 2l2.3 6.7L21 11l-6.7 2.3L12 20l-2.3-6.7L3 11l6.7-2.3L12 2Z M19 17v4 M17 19h4',
  monitor: 'M4 3h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1 M8 21h8 M12 17v4',
  moon: 'M21 13A9 9 0 0 1 11 3a9 9 0 1 0 10 10Z',
  bell: 'M18 8a6 6 0 0 0-12 0v7l-2 3h16l-2-3Z M10 21h4',
  paw: 'M8 14c-4 5-1 8 4 6 5 2 8-1 4-6-2-3-6-3-8 0Z M5 7a2 3 0 1 0 0 6 2 3 0 0 0 0-6 M19 7a2 3 0 1 0 0 6 2 3 0 0 0 0-6 M9 2a2 3 0 1 0 0 6 2 3 0 0 0 0-6 M15 2a2 3 0 1 0 0 6 2 3 0 0 0 0-6',
  info: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18 M12 11v6 M12 7v1',
}
</script>

<template>
  <svg :width="size || 20" :height="size || 20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="paths[name] || paths.info" /></svg>
</template>
