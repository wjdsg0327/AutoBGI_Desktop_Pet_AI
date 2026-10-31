<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { api, desktop, type AppState, type ProfileInput, type ProfileView } from '../bridge'
import { useAppStore } from '../stores/app'
import AppIcon from '../components/AppIcon.vue'
import PetCanvas from '../components/PetCanvas.vue'
import LiveLogPanel from '../components/LiveLogPanel.vue'

// 面板只编辑日志关键词规则，连接和日志状态始终以原生服务广播为准。
type RuleAction = 'idle' | 'happy' | 'wave' | 'surprised' | 'sad'
type TodoAction = '' | 'one_long' | 'script_group' | 'close_bgi_game' | 'shutdown'
type KeywordTodo = { enabled: boolean; action: TodoAction; target: string; autoApprove: boolean }
type KeywordRule = { id: string; keywords: string[]; action: RuleAction; text: string; enabled: boolean; todo?: KeywordTodo }
type RuleDraft = { id: string; keywordsText: string; action: RuleAction; text: string; enabled: boolean; todo: KeywordTodo }
type LogEntry = { time: string; text: string }
type LogStream = { status: string; fileName: string; logs: LogEntry[]; error: string; droppedTriggers?: number }
type PanelState = AppState & { keywordRules?: KeywordRule[]; logStream?: LogStream; preferences: AppState['preferences'] & { notifications: AppState['preferences']['notifications'] & { pickupEnabled?: boolean } } }

const store = useAppStore()
const petState = computed(() => store.state as PanelState | null)
const activeSection = computed(() => store.section === 'connections' ? 'connections' : store.section === 'logs' ? 'logs' : 'rules')
const stream = computed(() => petState.value?.logStream)
const streamStatus = computed(() => stream.value?.status || 'disconnected')
const streamLabel = computed(() => ({ online: '连接正常', connecting: '正在连接', backoff: '等待重连', paused: '连接已暂停', auth_failed: '密钥验证失败', error: '连接异常', disconnected: '尚未连接' })[streamStatus.value] || '状态未知')
const navigation = [
  { id: 'rules', label: '桌宠设置', icon: 'settings' },
  { id: 'connections', label: '连接设置', icon: 'link' },
  { id: 'logs', label: '实时日志', icon: 'list' },
]
const actions: { value: RuleAction; label: string }[] = [
  { value: 'idle', label: '安静陪伴' },
  { value: 'happy', label: '开心' },
  { value: 'wave', label: '挥手' },
  { value: 'surprised', label: '惊讶' },
  { value: 'sad', label: '难过' },
]
function navigate(section: string) { store.section = section === 'rules' ? 'settings' : section }

// 即时保存拾取开关；失败时继续显示宿主中实际保存的状态。
const pickupEnabled = computed(() => petState.value?.preferences.notifications.pickupEnabled === true)
async function setPickupEnabled(event: Event) {
  const input = event.target as HTMLInputElement
  const enabled = input.checked
  input.checked = pickupEnabled.value
  await store.run(async () => store.accept(await api.updatePickupEnabled(enabled)))
}

// 保存中的状态广播不能覆盖正在编辑的草稿；规则保存成功后再同步服务端副本。
const rules = ref<RuleDraft[]>([])
const rulesDirty = ref(false)
const applyingRules = ref(false)
const rulesNotice = ref('')
const previewAction = ref<RuleAction>('idle')
// 每次点击预览都递增序号，让相同动作也能重新开始播放。
const previewRevision = ref(0)
let previewTimer: ReturnType<typeof setTimeout> | undefined
// 旧规则没有自动审批字段时始终保持手动处理。
function normalizeTodo(todo?: Partial<KeywordTodo>): KeywordTodo {
  return { enabled: todo?.enabled === true, action: todo?.action || '', target: todo?.target || '', autoApprove: todo?.autoApprove === true }
}
function applyRules(items: KeywordRule[]) {
  applyingRules.value = true
  rules.value = items.map(item => ({ ...item, keywordsText: (item.keywords || []).join('，'), todo: normalizeTodo(item.todo) }))
  rulesDirty.value = false
  applyingRules.value = false
}
watch(() => JSON.stringify(petState.value?.keywordRules || []), () => {
  if (!rulesDirty.value) applyRules(petState.value?.keywordRules || [])
}, { immediate: true })
watch(rules, () => { if (!applyingRules.value) { rulesDirty.value = true; rulesNotice.value = '' } }, { deep: true, flush: 'sync' })

// 一行规则中的关键词按逗号或换行拆分，去重与后端的不区分大小写匹配保持一致。
function parseKeywords(value: string): string[] {
  const seen = new Set<string>()
  return value.split(/[,，\n\r]+/).map(item => item.trim()).filter(item => {
    const key = item.toLocaleLowerCase()
    if (!item || seen.has(key)) return false
    seen.add(key)
    return true
  })
}
function addRule() {
  if (rules.value.length >= 40) { rulesNotice.value = '最多可以设置 40 条规则。'; return }
  rules.value.push({ id: crypto.randomUUID(), keywordsText: '', action: 'wave', text: '', enabled: true, todo: normalizeTodo() })
}
function moveRule(index: number, offset: number) {
  const next = index + offset
  if (next < 0 || next >= rules.value.length) return
  const item = rules.value.splice(index, 1)[0]
  if (item) rules.value.splice(next, 0, item)
}
// 预览和正式关键词动作保持相同的五秒展示时间。
function preview(action: RuleAction) {
  clearTimeout(previewTimer)
  previewAction.value = action
  previewRevision.value++
  previewTimer = setTimeout(() => {
    previewAction.value = 'idle'
    previewRevision.value++
  }, 5000)
}
onBeforeUnmount(() => clearTimeout(previewTimer))

// 保存和导出共用草稿检查，避免分享不完整的规则。
function prepareRules(): KeywordRule[] | null {
  const prepared: KeywordRule[] = []
  for (const [index, rule] of rules.value.entries()) {
    const keywords = parseKeywords(rule.keywordsText)
    if (!keywords.length) { rulesNotice.value = `第 ${index + 1} 条规则请至少填写一个关键词。`; return null }
    if (keywords.length > 20 || keywords.some(item => [...item].length > 80)) { rulesNotice.value = `第 ${index + 1} 条规则最多填写 20 个关键词，每个不超过 80 个字符。`; return null }
    if ([...rule.text].length > 300) { rulesNotice.value = `第 ${index + 1} 条台词不能超过 300 个字符。`; return null }
    if (rule.todo.enabled && !rule.todo.action) { rulesNotice.value = `第 ${index + 1} 条规则请选择代办处理动作。`; return null }
    if (rule.todo.enabled && (rule.todo.action === 'one_long' || rule.todo.action === 'script_group') && !rule.todo.target.trim()) { rulesNotice.value = `第 ${index + 1} 条规则请选择代办的配置名称。`; return null }
    prepared.push({ id: rule.id, keywords, action: rule.action, text: rule.text, enabled: rule.enabled, todo: { ...rule.todo } })
  }
  return prepared
}

// 导入只追加草稿，超过数量上限时整批保留原样，保存后才开始匹配。
async function importRules() {
  const imported = await store.run(async () => (await desktop()).ImportKeywordRulesFile())
  if (!imported) return
  if (rules.value.length + imported.length > 40) {
    rulesNotice.value = `导入 ${imported.length} 条后将超过 40 条，请先删减规则。`
    return
  }
  for (const rule of imported) {
    rules.value.push({ id: crypto.randomUUID(), keywordsText: rule.keywords.join('，'), action: rule.action as RuleAction, text: rule.text, enabled: rule.enabled, todo: normalizeTodo(rule.todo ? { ...rule.todo, action: rule.todo.action as TodoAction } : undefined) })
  }
  rulesNotice.value = imported.length ? `已导入 ${imported.length} 条规则，请检查动作和自动审批设置后保存。` : '文件中没有规则。'
}

// 导出当前草稿，不要求先保存，也不包含连接信息。
async function exportRules() {
  const prepared = prepareRules()
  if (!prepared) return
  const saved = await store.run(async () => (await desktop()).ExportKeywordRulesFile(prepared))
  if (saved) rulesNotice.value = '规则文件已导出，可以分享给其他桌宠用户。'
}

// 服务端保存成功后才标记草稿已同步。
async function saveRules() {
  const prepared = prepareRules()
  if (!prepared) return
  const result = await store.run(async () => {
    // 规则仅通过宿主开放的精简业务绑定保存。
    const next = await api.updateKeywordRules(prepared) as PanelState
    store.accept(next)
    return next
  })
  if (result) {
    applyRules(result.keywordRules || [])
    rulesNotice.value = '规则已保存，后续收到的新日志会立即生效。'
  }
}

// 配置名称来自当前连接；连接切换会使旧读取结果和缓存同时失效。
const todoTargets = reactive<Record<'one_long' | 'script_group', string[]>>({ one_long: [], script_group: [] })
const todoLoading = reactive<Record<'one_long' | 'script_group', boolean>>({ one_long: false, script_group: false })
const todoTargetErrors = reactive<Record<'one_long' | 'script_group', string>>({ one_long: '', script_group: '' })
let todoTargetRevision = 0
watch(() => [store.state?.connection.profileId, store.state?.connection.generation, store.status], () => {
  todoTargetRevision++
  todoTargets.one_long = []
  todoTargets.script_group = []
  todoLoading.one_long = false
  todoLoading.script_group = false
  todoTargetErrors.one_long = ''
  todoTargetErrors.script_group = ''
})
async function loadTodoTargets(action: TodoAction) {
  if (action !== 'one_long' && action !== 'script_group') return
  if (!store.native || store.status !== 'online') { todoTargetErrors[action] = '请先连接 AutoBGI 再读取配置名称。'; return }
  if (todoLoading[action]) return
  const revision = todoTargetRevision
  todoLoading[action] = true
  todoTargetErrors[action] = ''
  try {
    const names = await api.listTodoTargets(action)
    if (revision === todoTargetRevision) todoTargets[action] = Array.isArray(names) ? names : []
  } catch (reason) {
    if (revision === todoTargetRevision) todoTargetErrors[action] = reason instanceof Error ? reason.message : '读取配置名称失败。'
  } finally {
    if (revision === todoTargetRevision) todoLoading[action] = false
  }
}
function changeTodoAction(rule: RuleDraft) {
  rule.todo.target = ''
  void loadTodoTargets(rule.todo.action)
}

// 地址和密钥通过原生接口保存，界面只显示脱敏资料，编辑完成后清除输入的密钥。
const editing = ref(false)
const deleting = ref('')
const connectionNotice = ref('')
const form = reactive<ProfileInput>({ id: '', name: 'AutoBGI', nickname: '', baseUrl: 'http://127.0.0.1:8082', apiKey: '', adapterMode: 'auto', readOnly: true, autoConnect: false, allowInsecure: false, sessionOnly: false })
// 已有连接只改昵称无需重新提供密钥，新增连接或更换地址仍必须填写。
const editingProfile = computed(() => store.state?.profiles?.find(item => item.id === form.id))
const editingNeedsKey = computed(() => !editingProfile.value || editingProfile.value.baseUrl !== form.baseUrl.trim())
const keyPlaceholder = computed(() => {
  if (editingNeedsKey.value) return '输入 auth.api_key'
  return editingProfile.value?.hasCredential ? '留空可保留已保存的密钥' : '仅修改昵称可留空，否则请输入 auth.api_key'
})
function editProfile(profile?: ProfileView) {
  // 编辑昵称时保留原有连接模式和权限，避免无关设置触发断线。
  Object.assign(form, { id: profile?.id || '', name: profile?.name || 'AutoBGI', nickname: profile?.nickname || '', baseUrl: profile?.baseUrl || 'http://127.0.0.1:8082', apiKey: '', adapterMode: profile?.adapterMode || 'auto', readOnly: profile?.readOnly ?? true, autoConnect: profile?.autoConnect || false, allowInsecure: profile?.allowInsecure || false, sessionOnly: profile?.sessionOnly || false })
  connectionNotice.value = ''
  deleting.value = ''
  editing.value = true
}
async function saveProfile() {
  const input = { ...form }
  try {
    if (await store.saveProfile(input)) {
      editing.value = false
      connectionNotice.value = isConnectionRunning(input.id) ? '连接资料已保存。' : '连接资料已保存，请点击“连接日志”。'
    }
  } finally { form.apiKey = ''; input.apiKey = '' }
}
async function removeProfile(id: string) {
  if (deleting.value !== id) { deleting.value = id; return }
  await store.deleteProfile(id)
  deleting.value = ''
}
function isConnectionRunning(id: string) {
  return store.state?.activeProfileId === id && ['online', 'connecting', 'backoff', 'paused'].includes(streamStatus.value)
}
function connectionButtonLabel(id: string) {
  if (isConnectionRunning(id)) return '断开连接'
  return store.state?.activeProfileId === id && streamStatus.value === 'auth_failed' ? '重新连接' : '连接日志'
}
function toggleConnection(id: string) {
  if (isConnectionRunning(id)) void store.disconnect()
  else void store.connect(id)
}
</script>

<template>
  <div class="furina-panel" :class="{ 'furina-panel--logs': activeSection === 'logs' }">
    <!-- 窗口内导航始终围绕关键词、连接和日志三个基础功能。 -->
    <aside class="furina-sidebar">
      <div class="brand-mark"><span class="brand-gem">A<span>·</span>P</span><div><strong>ABGI<span>-PET</span></strong><small>DESKTOP COMPANION</small></div></div>
      <p class="sidebar-caption">WORKSPACE <span>01 — 03</span></p>
      <nav aria-label="桌宠设置导航">
        <button v-for="item in navigation" :key="item.id" :class="{ active: activeSection === item.id }" @click="navigate(item.id)">
          <AppIcon :name="item.icon" :size="19" /><span><strong>{{ item.label }}</strong></span><b v-if="activeSection === item.id">●</b>
        </button>
      </nav>
      <div class="sidebar-status"><span class="light" :class="streamStatus" /><div><strong>{{ streamLabel }}</strong><small>ABGI CONNECTION</small></div></div>
      <div class="sidebar-foot"><span>ABGI-PET</span><small>让日志有一点温度</small></div>
    </aside>

    <main class="furina-main">
      <!-- 顶部说明保留当前连接状态，并提供回到桌宠窗口的入口。 -->
      <header class="topline"><div><span class="overline">ABGI-PET / CONTROL CENTER</span><h1>{{ activeSection === 'rules' ? '桌宠设置' : activeSection === 'connections' ? '连接设置' : '实时日志' }}</h1></div><div class="top-actions"><span class="connection-badge"><i class="light" :class="streamStatus" />{{ streamLabel }}</span><button class="round-button" title="显示桌宠" aria-label="显示桌宠" :disabled="!store.native || store.busy" @click="store.run(async () => (await desktop()).ShowPet())"><AppIcon name="paw" :size="17" /></button></div></header>
      <div v-if="!store.native" class="message info-message">当前是浏览器预览；连接、保存和桌宠动作需要在 Windows 桌宠中使用。</div>
      <div v-if="store.error || stream?.error || petState?.todoNotice" class="message error-message" role="alert"><span>{{ store.error || stream?.error || petState?.todoNotice }}</span><button v-if="store.error" aria-label="关闭提示" @click="store.error = ''"><AppIcon name="close" :size="15" /></button></div>
      <div v-if="store.nativeWarning" class="message error-message" role="alert">{{ store.nativeWarning }}</div>

      <!-- 规则按列表顺序处理，每条规则可包含多个关键词、动作和台词。 -->
      <section v-if="activeSection === 'rules'" class="page-content">
        <div class="hero"><div class="hero-copy"><span class="hero-kicker"><i />YOUR DESKTOP COMPANION</span><h2>让日志有回应，<br /><em>让陪伴更有趣。</em></h2><p>设置关键词、动作和台词，让芙宁娜陪你留意 ABGI 的每个瞬间。</p><button class="hero-button" :disabled="rules.length >= 40 || store.busy" @click="addRule"><AppIcon name="plus" :size="16" />添加关键词规则</button></div><div class="hero-portrait" :class="`pet-action-${previewAction}`"><div class="portrait-ring" /><span class="portrait-star star-a">✦</span><span class="portrait-star star-b">✧</span><PetCanvas :key="previewRevision" :action="previewAction" :skin-id="store.state?.preferences.pet.skinId" /><span class="portrait-caption">ABGI-PET / FURINA</span></div></div>
        <!-- 拾取开关独立于规则草稿，即时保存且显示宿主实际状态。 -->
        <div class="pickup-setting"><div><strong>拾取通知</strong><p>收到交互或拾取日志时，芙宁娜会吃掉写有物品名称的小蛋糕。</p></div><label class="toggle-label"><input type="checkbox" :checked="pickupEnabled" :disabled="!store.native || store.busy" @change="setPickupEnabled" /><span class="toggle" /><span class="sr-only">开启拾取通知</span></label></div>
        <div class="content-heading"><div><span class="overline">KEYWORD AUTOMATION</span><h2>关键词规则 <span>{{ rules.length }} / 40</span></h2><p>关键词不区分大小写；一条日志只执行第一条匹配规则。动作持续 5 秒后恢复待机，连续命中依次播放。</p></div><div class="rule-actions"><button class="secondary-button" :disabled="!store.native || store.busy" @click="importRules">导入规则</button><button class="secondary-button" :disabled="!store.native || store.busy" @click="exportRules">导出规则</button><button class="secondary-button" :disabled="rules.length >= 40 || store.busy" @click="addRule"><AppIcon name="plus" :size="16" />添加规则</button></div></div>
        <div v-if="rulesNotice" class="message info-message" role="status">{{ rulesNotice }}</div>
        <div v-if="!rules.length" class="empty-rules"><span class="empty-diamond">✧</span><h3>还没有关键词规则</h3><p>添加一条规则，为日志中的关键词指定动作和台词。</p><button class="secondary-button" :disabled="store.busy" @click="addRule">添加第一条规则 <AppIcon name="arrow" :size="15" /></button></div>
        <div v-else class="rule-list">
          <article v-for="(rule, index) in rules" :key="rule.id" class="rule-card">
            <div class="rule-top"><span class="rule-number">{{ String(index + 1).padStart(2, '0') }}</span><div><strong>关键词规则</strong><small>{{ rule.enabled ? '已启用' : '已暂停' }}</small></div><button class="order-button" :disabled="store.busy || index === 0" :aria-label="`上移第 ${index + 1} 条规则`" title="上移规则" @click="moveRule(index, -1)">↑</button><button class="order-button" :disabled="store.busy || index === rules.length - 1" :aria-label="`下移第 ${index + 1} 条规则`" title="下移规则" @click="moveRule(index, 1)">↓</button><label class="toggle-label"><input v-model="rule.enabled" type="checkbox" :disabled="store.busy" /><span class="toggle" /><span class="sr-only">启用规则 {{ index + 1 }}</span></label><button class="remove-button" :disabled="store.busy" :aria-label="`删除第 ${index + 1} 条规则`" @click="rules.splice(index, 1)"><AppIcon name="close" :size="16" /></button></div>
            <div class="rule-fields"><label class="rule-keywords">触发关键词 <small>多个关键词用逗号或换行分隔</small><textarea v-model="rule.keywordsText" :disabled="store.busy" rows="2" placeholder="例如：完成，结束" /></label><label>桌宠动作<select v-model="rule.action" :disabled="store.busy" @change="preview(rule.action)"><option v-for="action in actions" :key="action.value" :value="action.value">{{ action.label }}</option></select></label><label>要说的话 <small>留空则显示日志</small><input v-model="rule.text" :disabled="store.busy" maxlength="300" placeholder="例如：演出圆满结束！" /></label></div>
            <!-- 自动审批逐条启用，关闭代办时同时清除自动审批授权。 -->
            <div class="todo-rule">
              <label class="todo-toggle"><input v-model="rule.todo.enabled" type="checkbox" :disabled="store.busy" @change="!rule.todo.enabled && (rule.todo.autoApprove = false)" /><span>命中时创建代办</span></label>
              <div v-if="rule.todo.enabled" class="todo-rule-fields">
                <label>处理动作<select v-model="rule.todo.action" :disabled="store.busy" @change="changeTodoAction(rule)"><option value="">请选择动作</option><option value="one_long">启动一条龙</option><option value="script_group">启动配置组</option><option value="close_bgi_game">关闭 BGI 和原神</option><option value="shutdown">电脑关机</option></select></label>
                <label v-if="rule.todo.action === 'one_long' || rule.todo.action === 'script_group'">配置名称<select v-model="rule.todo.target" :disabled="store.busy" @focus="loadTodoTargets(rule.todo.action)"><option value="">请选择配置名称</option><option v-if="rule.todo.target && !todoTargets[rule.todo.action].includes(rule.todo.target)" :value="rule.todo.target">{{ rule.todo.target }}（已保存）</option><option v-for="name in todoTargets[rule.todo.action]" :key="name" :value="name">{{ name }}</option></select></label>
                <button v-if="rule.todo.action === 'one_long' || rule.todo.action === 'script_group'" class="text-action" type="button" :disabled="todoLoading[rule.todo.action]" @click="loadTodoTargets(rule.todo.action)">{{ todoLoading[rule.todo.action] ? '正在读取…' : '刷新配置名称' }}</button>
                <label class="todo-auto"><input v-model="rule.todo.autoApprove" type="checkbox" :disabled="store.busy" /><span>自动审批：命中后立即执行</span></label>
                <small v-if="rule.todo.autoApprove" class="todo-auto-hint">无需点击处理，将自动执行上方动作；同一连接正在处理其他请求时会排队。进行中的同规则命中会合并，完成后再次命中会再次执行；请求失败或结果未知时不会自动重发。</small>
                <small v-if="(rule.todo.action === 'one_long' || rule.todo.action === 'script_group') && todoTargetErrors[rule.todo.action]">{{ todoTargetErrors[rule.todo.action] }}</small>
                <small v-if="rule.todo.action === 'shutdown'">处理时会在 AutoBGI 所在电脑关闭相关软件，并安排 60 秒后关机。</small>
                <small v-else-if="!rule.todo.autoApprove">代办使用上方台词；同一规则重复命中会合并，保留至当天结束。</small>
              </div>
            </div>
            <div class="rule-bottom"><span>任意一个关键词出现在新日志中即可触发。</span><button :disabled="store.busy" @click="preview(rule.action)">预览动作 <AppIcon name="arrow" :size="14" /></button></div>
          </article>
        </div>
        <div class="save-bar"><span>{{ rulesDirty ? '修改尚未保存' : '规则已同步' }}</span><button class="primary-button" :disabled="!store.native || store.busy || !rulesDirty" @click="saveRules"><AppIcon name="check" :size="16" />保存规则</button></div>
      </section>

      <!-- API 密钥只在本次保存时提交，不在连接列表中回显。 -->
      <section v-else-if="activeSection === 'connections'" class="page-content">
        <div class="page-intro"><span class="overline">ABGI CONNECTION</span><h2>连接你的 ABGI</h2><p>填写 AutoBGI 服务地址和 API 密钥，连接成功后即可接收日志并触发规则。</p></div>
        <div v-if="connectionNotice" class="message success-message" role="status">{{ connectionNotice }}</div>
        <div v-for="profile in store.state?.profiles || []" :key="profile.id" class="connection-card">
          <div class="connection-icon"><AppIcon name="link" :size="21" /></div><div class="connection-details"><div class="connection-title"><strong>{{ profile.nickname?.trim() || profile.name }}</strong><span v-if="store.state?.activeProfileId === profile.id" class="mini-badge"><i class="light" :class="streamStatus" />{{ streamLabel }}</span></div><p>{{ profile.baseUrl }}</p><small>{{ profile.hasCredential ? profile.sessionOnly ? 'API 密钥仅用于本次会话' : 'API 密钥已保存' : '需要填写 API 密钥' }}{{ profile.autoConnect ? ' · 启动时自动连接' : '' }}</small></div><div class="connection-buttons"><button class="primary-button" :disabled="store.busy || !store.native" @click="toggleConnection(profile.id)">{{ connectionButtonLabel(profile.id) }}</button><button class="secondary-button" :disabled="store.busy || !store.native" @click="editProfile(profile)">编辑</button><button class="text-action danger" :disabled="store.busy" @click="removeProfile(profile.id)">{{ deleting === profile.id ? '确认删除' : '删除' }}</button></div>
        </div>
        <div v-if="!store.state?.profiles?.length && !editing" class="empty-rules"><span class="empty-diamond">✧</span><h3>还没有连接</h3><p>先填写 AutoBGI 地址和 API 密钥，开始接收实时日志。</p><button class="primary-button" :disabled="!store.native" @click="editProfile()">添加连接</button></div>
        <button v-if="store.state?.profiles?.length && !editing" class="secondary-button add-connection" :disabled="!store.native || store.busy" @click="editProfile()"><AppIcon name="plus" :size="16" />添加连接</button>
        <form v-if="editing" class="form-card" @submit.prevent="saveProfile"><div class="form-title"><div><span class="overline">CONNECTION DETAILS</span><h3>{{ form.id ? '编辑连接' : '添加连接' }}</h3></div><button type="button" class="remove-button" aria-label="取消编辑" @click="editing = false; form.apiKey = ''"><AppIcon name="close" :size="17" /></button></div><label>连接昵称<input v-model="form.nickname" maxlength="80" autocomplete="off" placeholder="可选，例如：我的电脑" /><small>用于标识连接和代办来源，留空时代办显示“未知”。</small></label><label>API 地址<input v-model="form.baseUrl" required maxlength="2048" autocomplete="off" spellcheck="false" placeholder="http://127.0.0.1:8082" /><small>使用 AutoBGI 启动时显示的实际服务地址。</small></label><label>API 密钥<input v-model="form.apiKey" type="password" :required="editingNeedsKey" maxlength="4096" autocomplete="new-password" :placeholder="keyPlaceholder" /><small>使用 AutoBGI 的 auth.api_key；修改地址后需重新填写密钥。</small></label><div class="check-options"><label><input v-model="form.autoConnect" type="checkbox" :disabled="form.sessionOnly" />启动桌宠时自动连接</label><label><input v-model="form.sessionOnly" type="checkbox" @change="form.sessionOnly && (form.autoConnect = false)" />密钥仅供本次会话</label></div><p v-if="form.sessionOnly" class="form-warning">会话密钥不会保存；下次启动时需要重新填写，无法自动连接。</p><div class="form-actions"><button type="button" class="secondary-button" @click="editing = false; form.apiKey = ''">取消</button><button class="primary-button" type="submit" :disabled="store.busy || !store.native">保存连接 <AppIcon name="arrow" :size="15" /></button></div></form>
      </section>

      <!-- 日志页只保留顶部标题，操作说明与接收限制分开，正文占满剩余空间。 -->
      <section v-else class="page-content logs-page">
        <p class="logs-hint">新日志自动跟随，向上滚动可暂停并查看历史。</p>
        <div v-if="(stream?.droppedTriggers || 0) > 0" class="message error-message" role="alert">有 {{ stream?.droppedTriggers }} 次触发未播放，请缩小关键词范围。</div>
        <LiveLogPanel :logs="stream?.logs || []" :status="streamStatus" :status-label="streamLabel" :file-name="stream?.fileName || ''" @configure="navigate('connections')" />
        <details class="logs-help"><summary>日志接收说明</summary><p>每次连接从当前日志末尾开始接收，最多保留最近 200 条。日志轮换及断线期间的内容可能漏收；关键词规则仅在收到新日志时触发。</p></details>
      </section>
      <footer class="panel-footer"><span>让每一条日志，都有回应。</span><span>ABGI-PET · DESKTOP COMPANION</span></footer>
    </main>
  </div>
</template>

<style scoped>
/* 日志页使用单一正文滚动区，标题与说明保持可见，其余设置页仍沿用自然页面滚动。 */
.furina-panel.furina-panel--logs{height:100dvh;min-height:440px}
.furina-panel--logs .furina-main{display:flex;flex-direction:column;min-height:0;height:100%}
.furina-panel--logs .topline,.furina-panel--logs .message{flex:none}
.furina-panel--logs .logs-page{display:flex;flex:1;flex-direction:column;min-height:0;animation:none}
.logs-hint{flex:none;margin:0 0 14px;color:#627d8e;font-size:12px;line-height:1.8}
.logs-help{flex:none;margin-top:12px;color:#627d8e;font-size:11px;line-height:1.8}.logs-help summary{width:fit-content;cursor:pointer}.logs-help p{margin-top:6px;overflow-wrap:anywhere}
.furina-panel--logs .panel-footer{flex:none;margin-top:14px;padding:12px 0 16px}
/* 导入、导出与新增操作在窄窗口中自动换行。 */
.rule-actions{display:flex;flex-wrap:wrap;gap:8px}
/* 代办设置沿用规则卡片的水蓝色与紧凑表单层次。 */
.todo-rule{margin-top:16px;padding:14px;border:1px solid #dcebf2;border-radius:10px;background:#f5fbfe}.todo-toggle{display:flex;align-items:center;gap:8px;color:#315f79;font-size:12px;font-weight:600}.todo-toggle input{accent-color:#498ca8}.todo-rule-fields{display:flex;align-items:end;flex-wrap:wrap;gap:10px;margin-top:12px}.todo-rule-fields label{display:flex;flex:1 1 160px;flex-direction:column;gap:7px;color:#557184;font-size:11px;font-weight:600}.todo-rule-fields select{width:100%;padding:9px;border:1px solid #dceaf1;border-radius:8px;background:#fff;color:#294b61;font-size:12px}.todo-rule-fields small{flex-basis:100%;color:#657f91;font-size:11px;line-height:1.6}
/* 自动审批说明与普通代办选项分开，提醒实际动作无需再次点击。 */
.todo-rule-fields .todo-auto{flex:1 1 100%;flex-direction:row;align-items:center;gap:8px;color:#315f79;font-size:12px}.todo-auto input{accent-color:#498ca8}.todo-rule-fields .todo-auto-hint{color:#9b6a45}
/* 海蓝、象牙白与细金线构成芙宁娜主题，避免依赖外部字体和网络图片。 */
.furina-panel{--ink:#203852;--muted:#7a8da1;--blue:#244c72;--line:#dbe5ec;--gold:#bc9c60;display:flex;min-height:100vh;color:var(--ink);background:radial-gradient(circle at 82% 6%,#e7f3f8 0,transparent 30%),#f3f7f9;font-family:"Microsoft YaHei UI","Microsoft YaHei",sans-serif}
.furina-sidebar{position:fixed;inset:0 auto 0 0;width:228px;padding:33px 19px 22px;background:linear-gradient(160deg,#18314e,#214765 70%,#193752);color:#e7f0f4;display:flex;flex-direction:column;z-index:2;overflow:auto}
.brand-mark{display:flex;align-items:center;gap:11px}.brand-gem{font-size:28px;color:#e6d4a5;display:grid;place-items:center;width:40px;height:40px;border:1px solid #b5a67a;border-radius:50% 50% 46% 46%;transform:rotate(45deg)}.brand-gem::first-letter{transform:rotate(-45deg)}.brand-mark strong{font-family:Georgia,serif;font-size:21px;letter-spacing:2px}.brand-mark small{display:block;font-family:Georgia,serif;font-size:7px;letter-spacing:1.4px;margin-top:5px;color:#a3bdcd}.sidebar-caption{font-family:Georgia,serif;font-size:8px;letter-spacing:1.9px;color:#9cb7c8;margin:48px 10px 15px}.furina-sidebar nav{display:flex;flex-direction:column;gap:7px}.furina-sidebar nav button{display:flex;align-items:center;text-align:left;gap:13px;padding:13px 12px;border:1px solid transparent;border-radius:9px;background:transparent;color:#b7cad6}.furina-sidebar nav button.active{background:#ffffff13;border-color:#ffffff1c;color:white}.furina-sidebar nav button:hover{background:#ffffff10}.furina-sidebar nav button span{flex:1}.furina-sidebar nav button strong{display:block;font-size:12px;font-weight:600}.furina-sidebar nav button small{display:block;font-size:9px;margin-top:5px;color:#9cb4c4}.furina-sidebar nav button b{font-size:11px;color:#e6d1a1}.sidebar-status{display:flex;gap:10px;align-items:center;margin-top:auto;padding:20px 8px;border-top:1px solid #ffffff22}.sidebar-status strong{display:block;font-size:11px}.sidebar-status small{display:block;margin-top:5px;font-family:Georgia,serif;font-size:8px;letter-spacing:1px;color:#91adbf}.sidebar-foot{padding:0 8px;font-family:Georgia,serif;font-size:9px;letter-spacing:1px;color:#a8bbc4}
.furina-main{width:calc(100% - 228px);max-width:1440px;margin-left:228px;padding:34px 42px 0}.topline{display:flex;align-items:center;justify-content:space-between;gap:15px;margin-bottom:27px}.overline{display:block;color:#8a9daf;font-family:Georgia,serif;font-size:9px;letter-spacing:2px}.topline h1{font-family:Georgia,"Microsoft YaHei",serif;font-size:26px;font-weight:500;margin-top:9px}.top-actions{display:flex;align-items:center;gap:11px}.connection-badge,.mini-badge{display:inline-flex;align-items:center;gap:7px;border:1px solid #dce8ec;border-radius:30px;background:#fff9;padding:8px 11px;color:#718999;font-size:10px}.light{display:inline-block;flex:none;width:7px;height:7px;border-radius:50%;background:#9aabba}.light.online{background:#6fb39e;box-shadow:0 0 0 3px #6fb39e24}.light.connecting,.light.backoff{background:#d5ab62}.light.auth_failed,.light.error{background:#d98276}.round-button,.remove-button{display:inline-grid;place-items:center;border:1px solid #dce7ed;background:#fff;color:#648499;border-radius:9px;padding:9px}.round-button:hover,.remove-button:hover{background:#eef6f9}
.light.paused{background:#d5ab62}
.message{display:flex;align-items:center;justify-content:space-between;gap:10px;border:1px solid #d9e5ea;background:#f7fbfc;color:#607e90;border-radius:10px;padding:13px 16px;font-size:11px;line-height:1.8;margin-bottom:16px}.message button{background:none;border:0;color:inherit}.error-message{background:#fff6f2;border-color:#efdcd5;color:#a16960}.success-message{background:#f2f9f7;border-color:#d5eae3;color:#558a7d}
/* 角色预览卡片和规则卡片保持清晰层次，桌宠动作由同一预览画布播放。 */
.page-content{animation:rise .35s ease both}.hero{position:relative;display:flex;min-height:270px;overflow:hidden;border:1px solid #d1e1ea;border-radius:16px;background:linear-gradient(115deg,#e8f1f5 0%,#dcebf1 62%,#cfe2eb 100%)}.hero::before{content:"";position:absolute;inset:11px;border:1px solid #ffffff80;border-radius:11px;pointer-events:none}.hero-copy{position:relative;z-index:1;max-width:65%;padding:36px 36px 32px}.hero-kicker{font-family:Georgia,serif;font-size:9px;letter-spacing:2px;color:#849bac}.hero h2{font-family:Georgia,"Microsoft YaHei",serif;font-size:28px;font-weight:500;line-height:1.5;margin:16px 0 10px}.hero h2 em{font-style:normal;color:#3f6b8b}.hero p{max-width:380px;color:#758e9f;font-size:11px;line-height:2}.hero-button,.primary-button,.secondary-button{display:inline-flex;align-items:center;justify-content:center;gap:8px;border-radius:8px;padding:10px 14px;font-size:11px;font-weight:600;white-space:nowrap}.hero-button,.primary-button{border:1px solid #294f6c;background:#294f6c;color:#fff}.hero-button{margin-top:20px}.hero-button:hover,.primary-button:hover{background:#183d5c}.secondary-button{background:#fff;border:1px solid #d6e4ea;color:#587a90}.secondary-button:hover{background:#eef6f9}.hero-portrait{position:absolute;right:4%;top:3%;bottom:4%;width:37%;display:flex;align-items:center;justify-content:center}.hero-portrait .pet-art{position:absolute;width:210px;height:230px;bottom:5px;z-index:1;filter:drop-shadow(0 12px 12px #50758c30)}.portrait-ring{position:absolute;width:220px;height:220px;border:1px solid #9bbac8;border-radius:50%;box-shadow:0 0 0 13px #ffffff2b,0 0 0 14px #ffffff70}.portrait-star{position:absolute;color:#b79b66;font-size:17px}.star-a{top:20px;right:15%}.star-b{bottom:35px;left:7%}.portrait-caption{position:absolute;bottom:5px;right:0;font-family:Georgia,serif;font-size:8px;letter-spacing:2px;color:#7898aa}
.content-heading{display:flex;justify-content:space-between;align-items:flex-end;gap:15px;margin:32px 0 17px}.content-heading h2{font-size:18px;font-weight:600;margin-top:8px}.content-heading h2 span{font-family:Georgia,serif;font-size:12px;color:#9aafbb;font-weight:400;margin-left:7px}.content-heading p,.page-intro p{font-size:11px;line-height:1.9;color:#8a9ead;margin-top:8px}.rule-list{display:grid;gap:12px}.rule-card,.form-card,.connection-card,.empty-rules{border:1px solid var(--line);background:#fff;border-radius:13px;box-shadow:0 5px 18px #264b6410}.rule-card{padding:20px 22px}.rule-top{display:flex;align-items:center;gap:12px}.rule-number{font-family:Georgia,serif;font-size:22px;color:#b9a077}.rule-top>div{flex:1}.rule-top strong{display:block;font-size:12px}.rule-top small{display:block;color:#9badb8;font-size:10px;margin-top:5px}.toggle-label{position:relative;cursor:pointer}.toggle-label input{position:absolute;opacity:0}.toggle{display:block;width:34px;height:20px;border-radius:20px;background:#d6e2e7;transition:background .2s}.toggle::after{content:"";display:block;width:14px;height:14px;position:relative;top:3px;left:3px;background:#fff;border-radius:50%;transition:transform .2s}.toggle-label input:checked+.toggle{background:#5c91a7}.toggle-label input:checked+.toggle::after{transform:translateX(14px)}.toggle-label input:focus-visible+.toggle{outline:3px solid #b8d6e4;outline-offset:2px}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0,0,0,0)}.rule-top .remove-button{padding:6px;border:0;color:#a9b7c0}.rule-fields{display:grid;grid-template-columns:1fr 1.35fr;gap:15px;margin-top:18px}.rule-keywords{grid-column:1/-1}.rule-fields label,.form-card>label{display:flex;flex-direction:column;gap:8px;font-size:11px;font-weight:600;color:#557184}.rule-fields label small{color:#a0b1bc;font-weight:400;margin-left:5px}.rule-fields textarea,.rule-fields input,.rule-fields select,.form-card input:not([type=checkbox]){width:100%;border:1px solid #dce8ed;border-radius:7px;background:#f9fcfd;color:#294b61;padding:11px 12px;outline:none;font-size:11px;font-weight:400}.rule-fields textarea{resize:vertical;min-height:60px}.rule-fields textarea:focus,.rule-fields input:focus,.rule-fields select:focus,.form-card input:focus{border-color:#83acc0;box-shadow:0 0 0 3px #b8d8e52b}.rule-bottom{display:flex;justify-content:space-between;align-items:center;gap:12px;border-top:1px solid #edf3f5;padding-top:14px;margin-top:16px;color:#a2b2bc;font-size:10px}.rule-bottom button,.text-action{display:flex;align-items:center;gap:6px;border:0;background:none;color:#608ca2;font-size:10px}.save-bar{display:flex;align-items:center;justify-content:flex-end;gap:15px;margin-top:18px;color:#9babb5;font-size:10px}.empty-rules{text-align:center;padding:36px 20px;color:#7b92a0}.empty-diamond{font-family:Georgia,serif;color:#c8ae7a;font-size:30px}.empty-rules h3{font-size:15px;font-weight:500;color:#48687e;margin:8px 0}.empty-rules p{font-size:11px;line-height:1.9;margin-bottom:17px}
.order-button{border:0;background:#f1f7f9;border-radius:7px;color:#6c91a3;padding:5px 8px;font-size:13px}
/* 拾取通知与关键词规则分开保存，外观沿用当前水蓝色卡片。 */
.pickup-setting{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-top:18px;padding:17px 20px;background:#fff;border:1px solid var(--line);border-radius:13px;box-shadow:0 5px 18px #264b6410}.pickup-setting strong{font-size:12px;color:#48687e}.pickup-setting p{font-size:10px;color:#8a9ead;line-height:1.8;margin-top:5px}
/* 连接表单展示凭据状态，页脚沿用统一的间距与主题颜色。 */
.page-intro{margin:4px 0 24px}.page-intro h2{font-family:Georgia,"Microsoft YaHei",serif;font-size:23px;font-weight:500;margin-top:12px}.page-intro p{max-width:520px}.connection-card{display:flex;align-items:center;gap:15px;padding:20px;margin-bottom:12px}.connection-icon{display:grid;place-items:center;flex:none;width:45px;height:45px;border-radius:12px;background:#eef6f8;color:#7098aa}.connection-details{min-width:0;flex:1}.connection-title{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.connection-title strong{font-size:13px}.connection-details p{font-family:Consolas,monospace;font-size:11px;color:#7993a3;margin-top:7px;overflow-wrap:anywhere}.connection-details small{display:block;color:#a0b0ba;font-size:10px;margin-top:7px}.mini-badge{font-size:9px;padding:4px 8px}.connection-buttons{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.danger{color:#b77b71}.add-connection{margin-top:9px}.form-card{padding:23px;margin-top:18px}.form-title{display:flex;justify-content:space-between;align-items:center;margin-bottom:21px}.form-title h3{font-size:15px;margin-top:8px}.form-card>label{margin-bottom:18px}.form-card>label small{font-weight:400;line-height:1.8;color:#9aadb9}.check-options{display:flex;flex-wrap:wrap;gap:12px 22px;font-size:11px;color:#6c8798}.check-options label{display:flex;gap:7px;align-items:center}.check-options input{accent-color:#467a92}.form-warning{font-size:10px;color:#b58068;line-height:1.8;margin-top:12px}.form-actions{display:flex;justify-content:flex-end;gap:9px;border-top:1px solid #edf3f5;padding-top:18px;margin-top:21px}.panel-footer{display:flex;justify-content:space-between;gap:10px;border-top:1px solid #dce8ed;margin-top:38px;padding:18px 0 22px;color:#a4b4bd;font-family:Georgia,serif;font-size:9px;letter-spacing:.7px}.panel-footer span:first-child{font-style:italic}@keyframes rise{from{opacity:0;transform:translateY(7px)}to{opacity:1;transform:translateY(0)}}
/* 窄窗口折叠侧栏和表单，仍保留所有编辑与连接入口。 */
@media(max-width:1000px){.furina-sidebar{width:190px;padding:28px 14px}.furina-main{width:calc(100% - 190px);margin-left:190px;padding:30px 25px 0}.hero-copy{padding:30px}.hero h2{font-size:24px}.hero-portrait{right:0}.hero-portrait .pet-art{width:185px;height:205px}.portrait-ring{width:180px;height:180px}.connection-card{flex-wrap:wrap}.connection-buttons{width:100%;padding-left:60px}}
@media(max-width:700px){.furina-sidebar{width:64px;padding:23px 9px}.brand-mark{justify-content:center}.brand-gem{width:38px;height:38px}.brand-mark div,.sidebar-caption,.furina-sidebar nav button span,.furina-sidebar nav button b,.sidebar-status div,.sidebar-foot{display:none}.furina-sidebar nav{margin-top:36px}.furina-sidebar nav button{justify-content:center;padding:13px 0}.sidebar-status{justify-content:center;padding:20px 0}.furina-main{width:calc(100% - 64px);margin-left:64px;padding:24px 18px 0}.topline h1{font-size:22px}.hero{min-height:320px}.hero-copy{max-width:100%;padding:26px}.hero h2{font-size:23px}.hero p{max-width:240px}.hero-portrait{top:auto;bottom:0;right:5px;width:145px;height:150px;opacity:.95}.hero-portrait .pet-art{width:140px;height:150px;bottom:0}.portrait-ring{width:130px;height:130px}.portrait-caption,.portrait-star{display:none}.content-heading{align-items:flex-start;flex-direction:column}.rule-fields{grid-template-columns:1fr}.rule-keywords{grid-column:auto}.connection-buttons{padding-left:0}}
@media(max-width:420px){.top-actions .round-button{display:none}.connection-badge{font-size:9px}.rule-card{padding:17px}.rule-bottom{align-items:flex-start;flex-direction:column}.save-bar{justify-content:space-between}.panel-footer{font-size:8px}}
@media(prefers-reduced-motion:reduce){.page-content{animation:none}.toggle,.toggle::after{transition:none}}
/* ABGI-PET 以深海蓝导航和清透蓝白卡片统一设置界面。 */
.furina-panel{--ink:#172f49;--muted:#70869b;--line:#dce8f0;background:radial-gradient(ellipse at 85% 2%,#dceffa 0,transparent 32%),linear-gradient(135deg,#f4f8fc,#edf4f8)}
.furina-sidebar{padding:30px 16px 20px;background:linear-gradient(165deg,#132d49 0%,#173b5b 62%,#102b43 100%);box-shadow:5px 0 24px #183c5812}
.brand-mark{gap:11px;padding:0 5px}.brand-gem{display:flex;align-items:center;justify-content:center;width:43px;height:43px;flex:none;transform:none;border:1px solid #8bc0dc;border-radius:14px;background:linear-gradient(145deg,#447faa,#224d72);box-shadow:inset 0 1px #ffffff40;color:#fff;font:700 13px Georgia,serif;letter-spacing:-1px}.brand-gem::first-letter{transform:none}.brand-gem span{color:#8cdcf0;padding:0 1px}.brand-mark strong{font:700 18px Georgia,serif;letter-spacing:.5px;white-space:nowrap}.brand-mark strong span{color:#8fd1e8}.brand-mark small{font:8px Georgia,serif;letter-spacing:1.3px;color:#91b2c8}
.sidebar-caption{display:flex;justify-content:space-between;margin:45px 12px 13px;font:9px Georgia,serif;letter-spacing:1.5px;color:#82a7bd}.sidebar-caption span{letter-spacing:.3px;opacity:.6}.furina-sidebar nav{gap:6px}.furina-sidebar nav button{padding:14px 12px;border-radius:11px;color:#aac3d3}.furina-sidebar nav button.active{background:linear-gradient(90deg,#4b88ac70,#3e789834);border-color:#8bc9e64a;color:#fff;box-shadow:inset 3px 0 #86c9e5}.furina-sidebar nav button small{color:#91aec1}.furina-sidebar nav button b{font-size:7px;color:#91d5e8}.sidebar-status{margin:auto 2px 0;padding:18px 10px;border-top:1px solid #ffffff20}.sidebar-status small{font:8px Georgia,serif;letter-spacing:1px;color:#83a4b8}.sidebar-foot{display:flex;flex-direction:column;gap:5px;padding:12px 12px 0;color:#80a2b6;font:9px Georgia,serif;letter-spacing:1px}.sidebar-foot small{font:10px "Microsoft YaHei",sans-serif;letter-spacing:0;color:#7899ad}
.furina-main{padding:32px 40px 0}.topline{margin-bottom:25px}.topline h1{font-family:"Microsoft YaHei UI","Microsoft YaHei",sans-serif;font-size:25px;font-weight:700;letter-spacing:.5px}.overline{color:#829caf;font-size:9px;letter-spacing:1.6px}.connection-badge{padding:9px 13px;border-color:#d6e7ef;background:#ffffffc9;color:#54778d}.round-button{border-radius:11px;box-shadow:0 3px 12px #244e6410}
.hero{min-height:255px;border-color:#c9e0ed;border-radius:18px;background:radial-gradient(circle at 83% 30%,#ffffffb8,transparent 29%),linear-gradient(110deg,#e8f5fb 0%,#d7eaf5 57%,#bcdceb 100%);box-shadow:0 9px 25px #366d8b12}.hero::before{inset:10px;border-color:#ffffff8a;border-radius:13px}.hero-copy{padding:34px 38px 30px}.hero-kicker{display:flex;align-items:center;gap:8px;color:#638ba2;font:9px Georgia,serif;letter-spacing:1.4px}.hero-kicker i{width:17px;height:2px;background:#5a9cbb}.hero h2{margin:17px 0 9px;font-family:"Microsoft YaHei UI","Microsoft YaHei",sans-serif;font-size:27px;font-weight:700;line-height:1.45;letter-spacing:.3px}.hero h2 em{color:#357b9d}.hero p{color:#62869a;font-size:11px}.hero-button,.primary-button{border-color:#246484;background:#246484;box-shadow:0 4px 10px #245e7a20}.hero-button:hover,.primary-button:hover{background:#174d6b}.secondary-button{border-color:#d4e5ed;color:#4b7d96}.hero-portrait .pet-art{filter:drop-shadow(0 10px 10px #376e8d38)}.portrait-ring{border-color:#83b4cd;box-shadow:0 0 0 13px #ffffff40,0 0 0 14px #ffffff80}.portrait-star{color:#5f9fba}.portrait-caption{color:#628da4;letter-spacing:1.3px}
.pickup-setting,.rule-card,.form-card,.connection-card,.empty-rules{border-color:#dce9f1;border-radius:14px;box-shadow:0 5px 18px #2c5a7410}.pickup-setting{padding:19px 22px}.pickup-setting strong{font-size:13px;color:#274b63}.content-heading{margin-top:31px}.content-heading h2{font-size:18px;color:#203e56}.content-heading p,.page-intro p{color:#718b9d}.rule-number{display:grid;place-items:center;width:38px;height:38px;border-radius:11px;background:#edf7fb;color:#4285a5;font-size:16px}.rule-fields textarea,.rule-fields input,.rule-fields select,.form-card input:not([type=checkbox]){background:#f8fbfd;border-color:#dceaf1;border-radius:8px}.toggle-label input:checked+.toggle{background:#4b99b6}.save-bar{padding:12px 0 0}.page-intro h2{font-family:"Microsoft YaHei UI","Microsoft YaHei",sans-serif;font-weight:700}.connection-icon{background:#e9f5fa;color:#438caf}.panel-footer{font-family:"Microsoft YaHei UI","Microsoft YaHei",sans-serif;letter-spacing:.3px}.panel-footer span:first-child{font-style:normal}
/* 辅助说明保持足够的字号与对比度，便于在桌面小窗口阅读。 */
.content-heading p,.page-intro p,.pickup-setting p,.connection-details small,.form-card>label small,.rule-fields label small,.rule-bottom,.save-bar{font-size:11px;color:#627d8e}.rule-top small{color:#6d8798}.rule-fields textarea,.rule-fields input,.rule-fields select,.form-card input:not([type=checkbox]){font-size:12px}.sidebar-status small,.sidebar-foot small{color:#a6c2d1}
/* 窄窗口继续显示紧凑导航，避免品牌和角色预览遮挡操作。 */
@media(max-width:1000px){.furina-sidebar{padding:28px 14px}.furina-main{padding:30px 25px 0}.brand-mark strong{font-size:16px}.hero-copy{padding:30px}}
@media(max-width:700px){.furina-sidebar{padding:23px 9px}.brand-mark{padding:0}.brand-gem{width:40px;height:40px;font-size:12px}.sidebar-caption,.sidebar-foot{display:none}.furina-sidebar nav button{padding:13px 0}.furina-main{padding:24px 18px 0}.hero{min-height:300px}.hero-copy{padding:26px}.hero h2{font-size:23px}}
</style>
