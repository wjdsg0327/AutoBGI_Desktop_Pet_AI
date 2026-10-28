<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api, desktop } from '../bridge'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 皮肤描述沿用 Go 生成的绑定类型，界面不会自行拼接本地文件路径。
type SkinInfo = NonNullable<Awaited<ReturnType<typeof api.listSkins>>['skins']>[number]
const store = useAppStore()
const skins = ref<SkinInfo[]>([])
const warning = ref('')
const message = ref('')
const loading = ref(false)
const pending = ref(false)
let alive = true
let catalogVersion = 0
const selectedID = computed(() => store.state?.preferences.pet.skinId || 'abgi-buddy')
const selected = computed(() => skins.value.find(item => item.id === selectedID.value))
const disabled = computed(() => !store.native || !store.state || store.busy || pending.value || loading.value)

// 列表读取使用独立版本号，初始化期间的共享操作锁不会吞掉首次加载。
async function loadCatalog() {
  if (!store.native) return
  const version = ++catalogVersion
  loading.value = true
  try {
    const catalog = await api.listSkins()
    if (!alive || version !== catalogVersion) return
    skins.value = catalog.skins || []
    warning.value = catalog.warning
  } catch (reason) {
    if (alive && version === catalogVersion) warning.value = reason instanceof Error ? reason.message : '皮肤列表暂不可用。'
  } finally {
    if (alive && version === catalogVersion) loading.value = false
  }
}

// 仅提交所选标识，由 Go 在当前配置上修改，保留其他窗口同时更新的偏好。
async function selectSkin(id: string) {
  if (!alive) return
  store.accept(await api.selectSkin(id))
}

// 切换只接受已校验目录中的标识，服务端会再次验证素材完整性。
async function changeSkin(id: string) {
  if (disabled.value || id === selectedID.value || !skins.value.some(item => item.id === id)) return
  pending.value = true
  message.value = ''
  try {
    await store.run(async () => {
      await selectSkin(id)
      if (alive) message.value = '角色外观已切换，桌宠和面板将使用同一皮肤。'
    })
  } finally {
    pending.value = false
  }
}

// 导入只通过原生 ZIP 选择器；取消操作没有成功提示，也不会切换当前角色。
async function importSkin() {
  if (disabled.value) return
  pending.value = true
  message.value = ''
  try {
    await store.run(async () => {
      const info = await (await desktop()).ImportSkin()
      if (!info || !alive) return
      message.value = `已导入「${info.name}」。如切换失败，可稍后从列表中选择。`
      await loadCatalog()
      if (!alive) return
      await selectSkin(info.id)
      if (alive) message.value = `已导入并使用「${info.name}」。`
    })
  } finally {
    pending.value = false
  }
}

// 只在宿主状态首次可用时加载；普通业务快照不会反复解码全部已安装素材。
watch(() => store.native && !!store.state, ready => { if (ready) void loadCatalog() }, { immediate: true })
onBeforeUnmount(() => { alive = false; catalogVersion++ })
</script>

<template>
  <!-- 沿用桌宠设置页的浅色卡片，将来源与许可保留为可核对的纯文本。 -->
  <div class="card setting-card skin-settings">
    <div class="setting-group-heading"><AppIcon name="paw" /><h3>角色外观</h3></div>
    <div class="setting-row">
      <div><strong>当前皮肤</strong><p>{{ loading ? '正在读取本地皮肤…' : selected ? `${selected.name}${selected.builtIn ? ' · 内置' : ' · 本地导入'}` : store.native ? '等待皮肤列表，内置角色仍可使用。' : '内置角色 · 叶芽 / ABGI 小助手' }}</p></div>
      <select class="compact-select" :value="selectedID" :disabled="disabled || !skins.length" aria-label="角色皮肤" @change="changeSkin(($event.target as HTMLSelectElement).value)">
        <option v-if="!skins.length" :value="selectedID">内置角色</option>
        <option v-for="item in skins" :key="item.id" :value="item.id">{{ item.name }}{{ item.builtIn ? ' · 内置' : '' }}</option>
      </select>
    </div>
    <div v-if="selected" class="skin-attribution"><span>作者：{{ selected.author }}</span><span>许可：{{ selected.license }}</span></div>
    <div class="setting-row">
      <div><strong>导入本地皮肤包</strong><p>ZIP 根目录需包含 manifest.json、atlas.png 和 LICENSE.txt。文件会先校验再安装；同名标识不能覆盖。</p></div>
      <button type="button" class="button button-light" :disabled="disabled" @click="importSkin"><AppIcon name="plus" :size="15" />{{ pending ? '处理中…' : '导入并使用' }}</button>
    </div>
    <p class="skin-hint">支持普通 PNG 精灵图，皮肤包不超过 8 MiB。仅导入你有权使用的素材；完整格式见随包附带的 docs/skins.md。</p>
    <p v-if="warning" class="skin-warning" role="alert">{{ warning }} <button type="button" class="text-button" :disabled="disabled" @click="loadCatalog">重新读取</button></p>
    <p v-if="message" class="skin-message" role="status">{{ message }}</p>
  </div>
</template>

<style scoped>
/* 皮肤说明在窄窗口内自然换行，避免长作者名或许可内容撑开设置面板。 */
.skin-attribution{display:flex;flex-wrap:wrap;gap:8px 22px;margin:-4px 0 12px;color:var(--muted,#8c9084);font-size:11px;overflow-wrap:anywhere}
.skin-hint,.skin-warning,.skin-message{margin:0 0 12px;font-size:11px;line-height:1.8;overflow-wrap:anywhere}
.skin-hint{color:var(--muted,#8c9084)}.skin-warning{color:#9c653b}.skin-message{color:#577843}.skin-warning .text-button{font-size:11px;margin-left:6px}
.skin-settings .compact-select{max-width:260px}.skin-settings .setting-row>div{min-width:0}.skin-settings .setting-row p{overflow-wrap:anywhere}
@media(max-width:680px){.skin-settings .compact-select{max-width:100%;width:100%}.skin-settings .setting-row{align-items:flex-start;flex-wrap:wrap;gap:12px}}
</style>
