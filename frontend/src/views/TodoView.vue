<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, desktop, listen, type AppState } from '../bridge'
import { useAppStore } from '../stores/app'

// 代办对话只消费宿主保存的快照，按钮永远不携带动作或连接参数。
type TodoItem = { id: string; text: string; action: string; target: string; profileId: string; baseUrl: string; createdAt: string; expiresAt: string; autoQueued?: boolean }
type TodoFeedback = { id: string; action: string; target: string; status: string; message: string; autoApproved?: boolean }
type TodoState = AppState & { todos?: TodoItem[]; todoFeedback?: TodoFeedback | null }
const store = useAppStore()
const expanded = ref(false)
const busy = ref(false)
const localError = ref('')
const items = computed(() => (store.state as TodoState | null)?.todos || [])
const item = computed(() => items.value[0])
// 按已校验的来源连接读取当前昵称，修改昵称后已有代办也同步显示。
const source = computed(() => store.state?.profiles?.find(profile => profile.id === item.value?.profileId))
const sourceName = computed(() => source.value?.nickname?.trim() || '未知')
// 地址只用于核对原始来源，不在代办对话中展示。
const sourceMatches = computed(() => !!item.value && !!source.value && source.value.baseUrl === item.value.baseUrl)
const feedback = computed(() => (store.state as TodoState | null)?.todoFeedback)
const todoNotice = computed(() => store.state?.todoNotice || '')
const canProcess = computed(() => !!item.value && sourceMatches.value && feedback.value?.status !== 'sending' && store.status === 'online' && store.state?.connection.profileId === item.value.profileId && store.state?.activeProfileId === item.value.profileId)
// 提示中保留刚提交的动作名称，避免与已经展示的下一条代办混淆。
function describeAction(current: { action: string; target: string } | undefined | null) {
  if (!current) return ''
  switch (current.action) {
    case 'one_long': return `启动一条龙：${current.target}`
    case 'script_group': return `启动配置组：${current.target}`
    case 'close_bgi_game': return '关闭 BGI 和原神'
    case 'shutdown': return '关闭相关软件，并安排电脑关机'
    default: return '未知动作'
  }
}
const actionText = computed(() => describeAction(item.value))
// 切换任务和远端关机的影响在用户决定前明确展示。
const impactText = computed(() => {
  switch (item.value?.action) {
    case 'one_long': return '可能中断正在运行的 BGI 任务。'
    case 'script_group': return '会切换当前 BGI 任务。'
    case 'shutdown': return '操作对象是此 AutoBGI 所在电脑，关闭软件后安排 60 秒关机倒计时。'
    default: return ''
  }
})
let disposed = false
let stopWindow: (() => void) | undefined
let windowRevision = -1
let hoverWanted = false
let hoverReported = false
let hoverSending = false

// 点击后使用宿主返回的新快照，避免状态广播迟到时重复处理同一条代办。
async function decide(process: boolean) {
  const current = item.value
  if (!current || busy.value || process && !canProcess.value) return
  busy.value = true
  localError.value = ''
  try {
    const next = process ? await api.processTodo(current.id) : await api.dismissTodo(current.id)
    store.accept(next)
  } catch (reason) {
    localError.value = reason instanceof Error ? reason.message : '这件事暂时没有处理完成，请稍后再试。'
  } finally { busy.value = false }
}

// 事件和补取快照可能乱序，较早的窗口模式不能覆盖新状态。
function updateWindow(value: { visible?: boolean; expanded?: boolean; revision?: number }) {
  if (!Number.isSafeInteger(value?.revision) || value.revision! <= windowRevision) return
  windowRevision = value.revision!
  expanded.value = value.expanded === true
  if (!value.visible) {
    localError.value = ''
    hoverWanted = false
    if (!hoverSending) hoverReported = false
  }
}
// 进入和离开必须串行，迟到的离开上报不能覆盖后一次进入。
async function hover(value: boolean) {
  hoverWanted = value
  if (!store.native || disposed || hoverSending) return
  hoverSending = true
  try {
    while (!disposed && hoverReported !== hoverWanted) {
      const next = hoverWanted
      await (await desktop()).SetTodoWindowHover(next)
      hoverReported = next
    }
  } catch { hoverReported = false }
  finally { hoverSending = false }
}
// 展开和收起只切换本地窗口，不消费代办。
function open() { if (store.native) void store.runLocal(async () => (await desktop()).OpenTodoWindow()) }
function close() { if (store.native) void store.runLocal(async () => (await desktop()).CloseTodoWindow()) }

// 先订阅再补取窗口状态，确保首次建窗时不会错过展示模式。
onMounted(async () => {
  const stop = await listen<{ visible: boolean; expanded: boolean; revision: number }>('pet:todo-window', updateWindow)
  if (disposed) { stop(); return }
  stopWindow = stop
  if (store.native) {
    try { updateWindow(await (await desktop()).GetTodoWindowState()) } catch { /* 宿主退出时无需恢复窗口。 */ }
  }
})
onBeforeUnmount(() => { disposed = true; stopWindow?.() })
</script>

<template>
  <main class="todo-surface" @pointerenter="hover(true)" @pointerleave="hover(false)">
    <!-- 圆圈和对话使用同一个独立窗口，避免透明台词气泡吞掉按钮。 -->
    <button v-if="!expanded" class="todo-orb" type="button" :aria-label="`代办，${items.length} 条`" @click="open">
      <span class="orb-star">✦</span><span>代办</span><b>{{ items.length }}</b>
    </button>
    <section v-else class="todo-card" aria-label="芙宁娜的代办对话">
      <header class="todo-heading"><div><span>FURINA'S NOTES</span><strong>芙宁娜的代办</strong></div><button type="button" aria-label="收起代办" @click="close">×</button></header>
      <div v-if="item" class="todo-body">
        <div class="todo-kicker"><span>✦</span> 这件事，还等着你拿主意呢！ <b>{{ items.length }} 条</b></div>
        <div class="todo-message">{{ item.text }}</div>
        <div class="todo-action"><span>处理时</span><strong>{{ actionText }}</strong></div>
        <p v-if="impactText" class="todo-impact">{{ impactText }}</p>
        <p class="todo-source">来源：{{ sourceName }}</p>
        <p v-if="item.autoQueued" class="todo-error">自动审批正在排队，执行前可以选择不管。</p>
        <p v-else-if="feedback?.status === 'sending'" class="todo-error">上一条请求仍在处理，可以先选择不管这条。</p>
        <p v-else-if="!sourceMatches" class="todo-error">这条代办的来源已变更，只能选择不管。</p>
        <p v-else-if="!canProcess" class="todo-error">请先连接这条代办所属的 AutoBGI，或选择不管。</p>
      </div>
      <div v-else class="todo-empty"><span>✧</span><strong>{{ todoNotice ? '代办暂时无法读取或保存。' : '今天的代办都落幕啦。' }}</strong><p>{{ todoNotice ? '请检查本地数据目录。' : '哼哼，本小姐可是记得很清楚！' }}</p></div>
      <p v-if="localError || todoNotice || feedback?.message" class="todo-feedback" :class="{ error: localError || todoNotice || feedback?.status === 'error' }" role="status">{{ localError || todoNotice || `${feedback?.autoApproved ? '自动审批' : '刚才'}的${describeAction(feedback)}：${feedback?.message}` }}</p>
      <footer v-if="item" class="todo-actions"><button type="button" class="todo-ignore" :disabled="busy" @click="decide(false)">不管</button><button type="button" class="todo-process" :disabled="busy || !canProcess" @click="decide(true)">{{ busy ? '请稍候' : '处理' }}</button></footer>
    </section>
  </main>
</template>

<style scoped>
/* 海蓝、象牙白和细金线延续角色风格，长日志在卡片内部滚动。 */
.todo-surface{width:100%;height:100%;padding:3px;font-family:"Microsoft YaHei UI","Microsoft YaHei",sans-serif;color:#173d5c;user-select:none}.todo-orb{position:relative;display:flex;width:68px;height:68px;align-items:center;justify-content:center;flex-direction:column;gap:1px;border:1px solid #b6d7e7;border-radius:50%;background:radial-gradient(circle at 30% 20%,#fff,#e3f5fd 64%,#b9dae9);color:#285a7d;box-shadow:0 3px 10px #1e516647,inset 0 0 0 4px #ffffff70;font-size:13px;font-weight:700;cursor:pointer}.todo-orb:focus-visible,.todo-card button:focus-visible{outline:2px solid #3e91bb;outline-offset:2px}.orb-star{color:#ad925f;font-size:11px;line-height:1}.todo-orb b{position:absolute;right:-2px;top:0;min-width:20px;height:20px;padding:0 4px;border:2px solid #f7fcff;border-radius:12px;background:#276d93;color:#fff;font-size:10px;line-height:16px;text-align:center}.todo-card{display:flex;flex-direction:column;height:100%;overflow:hidden;border:1px solid #bad7e6;border-radius:17px;background:linear-gradient(155deg,#fff 0%,#f4fbff 55%,#e6f4fa 100%);box-shadow:0 5px 15px #17486d3a}.todo-heading{display:flex;align-items:center;justify-content:space-between;flex:none;padding:12px 15px 9px;border-bottom:1px solid #d9ebf2}.todo-heading div{display:flex;align-items:baseline;gap:8px}.todo-heading span{color:#9a916d;font:8px Georgia,serif;letter-spacing:1px}.todo-heading strong{font-size:13px}.todo-heading button{width:25px;height:25px;border:0;border-radius:50%;background:#eaf5fa;color:#5c849a;font-size:20px;line-height:1;cursor:pointer}.todo-body{flex:1;min-height:0;overflow:auto;padding:10px 15px 4px}.todo-kicker{display:flex;align-items:center;gap:5px;color:#527f9a;font-size:11px}.todo-kicker span{color:#b49c67}.todo-kicker b{margin-left:auto;color:#84a4b5;font-size:10px;font-weight:400}.todo-message{max-height:76px;overflow:auto;margin:8px 0;padding:9px 10px;border-radius:9px;background:#fff;color:#254f68;font-size:12px;line-height:1.55;white-space:pre-wrap;overflow-wrap:anywhere}.todo-action{display:flex;gap:8px;align-items:flex-start;color:#436b82;font-size:11px;line-height:1.5}.todo-action span{flex:none;color:#88a4b1}.todo-action strong{overflow-wrap:anywhere}.todo-impact,.todo-source,.todo-error{margin:4px 0 0;font-size:10px;line-height:1.5;overflow-wrap:anywhere}.todo-impact{color:#a17750}.todo-source{color:#819aa9}.todo-error{color:#ab5c52}.todo-empty{display:flex;flex:1;align-items:center;justify-content:center;flex-direction:column;gap:8px;color:#638da3;font-size:11px}.todo-empty span{color:#b5a072;font-size:28px}.todo-empty strong{color:#365f79;font-size:13px}.todo-empty p{margin:0}.todo-feedback{flex:none;max-height:42px;overflow:auto;margin:0 15px 5px;color:#527f76;font-size:10px;line-height:1.5}.todo-feedback.error{color:#ae6559}.todo-actions{display:flex;flex:none;justify-content:flex-end;gap:8px;padding:8px 14px 12px;border-top:1px solid #dcecf2}.todo-actions button{min-width:76px;padding:8px 12px;border-radius:8px;font-size:11px;font-weight:600;cursor:pointer}.todo-actions button:disabled{opacity:.45;cursor:default}.todo-ignore{border:1px solid #cbdfe8;background:#fff;color:#628499}.todo-process{border:1px solid #286987;background:#286987;color:#fff}
</style>
