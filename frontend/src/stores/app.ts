import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, desktop, isNative, listen, type AppState, type ProfileInput, type NativePresentation } from '../bridge'

// 所有窗口只订阅同一份服务状态，不通过前端计时器重复轮询接口。
export const useAppStore = defineStore('app', () => {
  const state = ref<AppState | null>(null)
  const error = ref('')
  const nativeWarning = ref('')
  const busy = ref(false)
  const native = isNative()
  const section = ref('overview')
  const panelDestination = ref({ target: '', revision: 0 })
  const bubble = ref({ text: '', kind: 'info' })
  // 原生控制台在宿主确认前按隐藏处理；浏览器预览保留当前页面的可见默认值。
  const presentation = ref({ visible: true, panelVisible: !native, action: '' })
  const cleanups: (() => void)[] = []
  let disposed = false
  let presentationRevision = -1

  const status = computed(() => state.value?.connection.status || 'disconnected')
  const statusLabel = computed(() => ({ online: '连接正常', connecting: '正在连接', paused: '已暂停同步', backoff: '等待重连', auth_failed: '密钥验证失败', incompatible: '协议不兼容', disconnected: '尚未连接' })[status.value] || '状态未知')

  // 版本与连接代次共同排序，迟到的读取或广播不能覆盖较新的状态。
  function accept(next: AppState) {
    if (state.value && (next.revision <= state.value.revision || next.connection.generation < state.value.connection.generation)) return
    state.value = next
  }

  // 原生显隐独立于网络快照排序，旧广播不能恢复过时的窗口状态。
  function acceptPresentation(value: NativePresentation) {
    if (!Number.isSafeInteger(value.revision) || value.revision < 0 || value.revision <= presentationRevision) return
    presentationRevision = value.revision
    presentation.value = { visible: value.visible, panelVisible: value.panelVisible === true, action: value.action || '' }
    if (typeof value.warning === 'string') nativeWarning.value = value.warning
    // 独立目的地版本只消费显式入口；普通显隐广播保留面板中的选择。
    const revision = value.panelSectionRevision
    const destination = value.panelSection
    const target = value.panelTarget
    if (typeof revision === 'number' && Number.isSafeInteger(revision) && revision > panelDestination.value.revision &&
      ((destination === 'overview' || destination === 'connections') && target === '' ||
        destination === 'settings' && target === 'notifications')) {
      section.value = destination
      panelDestination.value = { target, revision }
    }
  }

  // 初始化读取也携带宿主版本，迟到的快照不能覆盖更新的原生广播。
  function acceptHostPresentation(initial: Record<string, unknown>) {
    acceptPresentation({
      visible: initial.visible !== false, panelVisible: initial.panelVisible === true,
      action: typeof initial.action === 'string' ? initial.action : '',
      revision: typeof initial.revision === 'number' ? initial.revision : -1,
      warning: typeof initial.warning === 'string' ? initial.warning : '',
      panelSection: initial.panelSection, panelTarget: initial.panelTarget, panelSectionRevision: initial.panelSectionRevision,
    })
  }

  // 窗口操作共用忙碌状态与错误出口，避免重复提交。
  async function run<T>(work: () => Promise<T>): Promise<T | undefined> {
    if (busy.value) return
    busy.value = true
    error.value = ''
    try { return await work() }
    catch (reason) {
      error.value = reason instanceof Error ? reason.message : '操作未完成，请稍后重试。'
    }
    finally { busy.value = false }
  }

  // 原生菜单属于即时本地入口，不能因为面板正在保存或初始化就被全局 busy 丢弃。
  async function runLocal<T>(work: () => Promise<T>): Promise<T | undefined> {
    error.value = ''
    try { return await work() }
    catch (reason) {
      error.value = reason instanceof Error ? reason.message : '本地操作未完成，请稍后重试。'
    }
  }

  // 先订阅事件再读取快照，并在卸载后停止使用迟到的初始化结果。
  async function initialize() {
    if (!native) return
    disposed = false
    await run(async () => {
      let bubbleVersion = 0
      const offState = await listen<AppState>('pet:state', accept)
      const offBubble = await listen<{ text: string; kind?: string }>('pet:bubble', value => { bubbleVersion++; bubble.value = { text: value.text, kind: value.kind || 'info' } })
      const offPresentation = await listen<NativePresentation>('pet:presentation', acceptPresentation)
      if (disposed) { offState(); offBubble(); offPresentation(); return }
      cleanups.push(offState, offBubble, offPresentation)
      const initialState = await api.getState()
      if (disposed) return
      accept(initialState)
      // 新建窗口可能晚于第一条广播，主动补取无敏感数据的展示快照。
      const initial = await (await desktop()).GetPresentation() || {}
      if (disposed) return
      acceptHostPresentation(initial)
      if (bubbleVersion === 0) bubble.value = { text: typeof initial.text === 'string' ? initial.text : '', kind: typeof initial.kind === 'string' ? initial.kind : 'info' }
    })
  }

  // 连接操作只接收服务端返回的脱敏状态。
  function dispose() { disposed = true; cleanups.splice(0).forEach(stop => stop()) }
  async function connect(id: string) { await run(async () => accept(await api.connect(id))) }
  async function disconnect() { await run(async () => accept(await api.disconnect())) }
  async function saveProfile(input: ProfileInput) { return run(async () => { accept(await api.saveProfile(input)); return true }) }
  async function deleteProfile(id: string) { await run(async () => accept(await api.deleteProfile(id))) }

  return { state, error, nativeWarning, busy, native, section, bubble, presentation, status, statusLabel, accept, initialize, dispose, run, runLocal, connect, disconnect, saveProfile, deleteProfile }
})
