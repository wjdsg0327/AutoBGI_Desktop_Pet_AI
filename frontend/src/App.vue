<script setup lang="ts">
import { defineAsyncComponent, defineComponent, h, onBeforeUnmount, onMounted } from 'vue'
import { useAppStore } from './stores/app'

// 视图模块异步失败时提供可见出口，避免 mount 返回后留下无提示的空白窗口。
const ViewLoadError = defineComponent({
  setup: () => () => h('div', { class: 'startup-error', role: 'alert' }, [
    h('p', '窗口资源加载失败，请重新加载此窗口，或从托盘退出后重新打开桌宠。'),
    h('button', { type: 'button', onClick: () => location.reload() }, '重新加载此窗口'),
  ]),
})

// 每种原生窗口仅加载自身视图，角色启动时不提前解析控制台及业务表格。
const PanelView = defineAsyncComponent({ loader: () => import('./views/PanelView.vue'), errorComponent: ViewLoadError })
const PetView = defineAsyncComponent({ loader: () => import('./views/PetView.vue'), errorComponent: ViewLoadError })
const BubbleView = defineAsyncComponent({ loader: () => import('./views/BubbleView.vue'), errorComponent: ViewLoadError })
const TodoView = defineAsyncComponent({ loader: () => import('./views/TodoView.vue'), errorComponent: ViewLoadError })

// 原生宿主明确指定窗口职责，浏览器预览默认打开信息面板。
const view = new URLSearchParams(location.search).get('view') || 'panel'
const store = useAppStore()
document.documentElement.dataset.view = view
onMounted(() => store.initialize())
onBeforeUnmount(() => store.dispose())
</script>

<template>
  <PetView v-if="view === 'pet'" />
  <BubbleView v-else-if="view === 'bubble'" />
  <TodoView v-else-if="view === 'todo'" />
  <PanelView v-else />
</template>
