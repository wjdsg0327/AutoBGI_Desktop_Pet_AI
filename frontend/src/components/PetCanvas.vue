<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { SpriteRenderer } from '../pet/renderer'

// 动画只接收展示状态，播放完成不会反向写入连接或任务状态。
const props = withDefaults(defineProps<{ action?: string; visible?: boolean; preview?: boolean; skinId?: string; pickupName?: string }>(), { action: 'idle', visible: true, preview: false, pickupName: '' })
const emit = defineEmits<{ ready: []; pickupReady: [] }>()
const canvas = ref<HTMLCanvasElement>()
const failed = ref(false)
let renderer: SpriteRenderer | null = null
let loadVersion = 0

// 当前版本只加载内置芙宁娜素材，旧版所选皮肤不会覆盖新主题。
async function loadSkin() {
  const current = renderer
  if (!current) return
  const version = ++loadVersion
  try {
    if (await current.load('/pets/furina/manifest.json') && version === loadVersion) {
      failed.value = false
      // 图片解码和渲染器初始化完成，父组件可以安排动作首帧确认。
      emit('ready')
    }
  } catch {
    // 素材缺失时保留文字入口，让用户仍能操作原生托盘。
    if (version === loadVersion) failed.value = !current.hasImage
  }
}
onMounted(async () => {
  if (!canvas.value) return
  renderer = new SpriteRenderer(canvas.value)
  // 面板预览在素材加载前关闭动画，加载中的动作和显隐变化仍由同一渲染器接收。
  renderer.setAnimated(!props.preview)
  renderer.setAction(props.action)
  renderer.setVisible(props.visible)
  await loadSkin()
})
watch(() => props.action, value => renderer?.setAction(value))
watch(() => props.visible, value => renderer?.setVisible(value))
watch(() => props.preview, value => renderer?.setAnimated(!value))
onBeforeUnmount(() => { loadVersion++; renderer?.destroy(); renderer = null })
</script>

<template>
  <div class="pet-art" :class="{ 'pet-art-preview': preview }">
    <canvas v-show="!failed && action !== 'pickup'" ref="canvas" aria-label="芙宁娜桌宠" role="img" />
    <!-- 食物名称以普通文本叠加，碎屑只参与动画，不影响鼠标操作。 -->
    <div v-show="action === 'pickup' && visible" class="pickup-art">
      <img class="pickup-character" src="/pets/furina/pickup.png" alt="芙宁娜吃小蛋糕" @load="emit('pickupReady')" @error="emit('pickupReady')" />
      <span class="pickup-crumb pickup-crumb-one" aria-hidden="true" />
      <span class="pickup-crumb pickup-crumb-two" aria-hidden="true" />
      <span class="pickup-crumb pickup-crumb-three" aria-hidden="true" />
      <span class="pickup-name" :aria-label="pickupName">{{ pickupName }}</span>
    </div>
    <div v-if="failed" class="pet-fallback" role="img" aria-label="内置角色资源加载失败">芙宁娜<span>角色素材暂不可用</span></div>
  </div>
</template>
