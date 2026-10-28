<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import type { NotificationHistoryScope } from '../bridge'
import { notificationErrorSummary } from '../pet/actions'
import { parseNotificationTime } from '../pet/actionTime'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 历史与实时气泡分别读取；切换筛选或打开面板不会隐式把通知设为已读。
const store = useAppStore()
const unreadOnly = ref(false)
const pendingConfirmation = ref('')
const limits = reactive<Record<string, number>>({})
const history = computed(() => store.state?.notificationHistory)
const unreadCount = computed(() => (history.value?.scopes || []).reduce((count, scope) => count + scope.unreadCount + (notificationErrorSummary(scope)?.unreadCount || 0), 0))
const scopes = computed(() => (history.value?.scopes || []).filter(scope => ((scope.entries?.length || 0) > 0 || scope.truncated || scope.quietPending > 0 || errorSummary(scope)) &&
  (!unreadOnly.value || scope.unreadCount > 0 || scope.quietPending > 0 || errorSummary(scope))))

// 当前来源集合、摘要版本和数量一旦变化便撤回本地确认，旧页面按钮不能确认新发生的错误。
const confirmationContext = computed(() => JSON.stringify([
  store.state?.activeProfileId, store.profile?.baseUrl, store.state?.connection.profileId, store.state?.connection.generation,
  store.state?.capabilities?.sourceMode, store.state?.capabilities?.instanceId, history.value?.profileId, history.value?.baseUrl,
  (history.value?.scopes || []).map(scope => [scopeKey(scope), scope.unreadCount, scope.quietPending, notificationErrorSummary(scope)]),
]))
watch([confirmationContext, unreadOnly, () => store.section], () => { pendingConfirmation.value = '' }, { flush: 'sync' })
onBeforeUnmount(() => { pendingConfirmation.value = '' })

// 完整元组作为界面键，旧实例、账号和探测前的本地错误记录不能合并成当前身份。
function scopeKey(scope: NotificationHistoryScope) {
  return JSON.stringify([scope.profileId, scope.baseUrl, scope.sourceMode, scope.instanceId, scope.accountId])
}
function entries(scope: NotificationHistoryScope) {
  return [...(scope.entries || [])].reverse().filter(entry => !unreadOnly.value || !entry.readAt)
}
function visibleEntries(scope: NotificationHistoryScope) {
  return entries(scope).slice(0, limits[scopeKey(scope)] || 20)
}
function showMore(scope: NotificationHistoryScope) {
  const key = scopeKey(scope)
  limits[key] = (limits[key] || 20) + 20
}
function sourceLabel(scope: NotificationHistoryScope) {
  if (scope.sourceMode === 'unverified') return '连接验证前的本地提醒'
  if (scope.sourceMode === 'legacy') return 'Legacy 连接记录'
  return `服务实例 ${scope.instanceId.slice(0, 12)}${scope.instanceId.length > 12 ? '…' : ''}`
}
function formatTime(value: string | null | undefined) {
  if (!value) return '时间未知'
  // 校验与展示共用存储兼容规则；偏移跨入公元前时明确纪元，避免把公元前一年显示成公元一年。
  const date = parseNotificationTime(value)
  return date ? date.toLocaleString('zh-CN', { hour12: false, ...(date.getFullYear() <= 0 ? { era: 'short' } as const : {}) }) : '时间不可用'
}

// 仅非空有效摘要提供确认入口，零计数版本仍由后端保留用于防止旧请求重放。
function errorSummary(scope: NotificationHistoryScope) {
  const summary = notificationErrorSummary(scope)
  return summary && summary.unreadCount > 0 ? summary : null
}

// 本地二步确认绑定完整来源与当前摘要事实，不把摘要伪装成正文编号。
function confirmationKey(scope: NotificationHistoryScope) {
  const summary = errorSummary(scope)
  return summary ? JSON.stringify([scopeKey(scope), summary.revision, summary.unreadCount, summary.quietPending, summary.latestAt]) : ''
}
function prepareAcknowledgement(scope: NotificationHistoryScope) {
  if (!store.native || store.busy) return
  pendingConfirmation.value = confirmationKey(scope)
}
function acknowledgeErrors(scope: NotificationHistoryScope) {
  const key = confirmationKey(scope)
  if (!store.native || store.busy || !key || key !== pendingConfirmation.value) {
    pendingConfirmation.value = ''
    return
  }
  pendingConfirmation.value = ''
  void store.acknowledgeNotificationErrors(scope)
}

// 批量操作只覆盖当前来源中实际保留的未读编号，不用“全部”去清除其他来源。
function markScopeRead(scope: NotificationHistoryScope) {
  const ids = (scope.entries || []).filter(entry => !entry.readAt).map(entry => entry.id)
  if (ids.length) void store.markNotificationsRead(scope, ids)
}
</script>

<template>
  <section class="card notification-inbox" aria-label="本地通知历史">
    <div class="card-heading">
      <h3><AppIcon name="bell" :size="18" />通知记录</h3>
      <span class="subtle-badge">{{ unreadCount }} 条未读</span>
    </div>
    <p class="inbox-description">提醒保存在本机，重启后仍可查看。查看记录或关闭气泡不会自动标为已读。</p>
    <div class="inbox-filter" role="group" aria-label="通知筛选">
      <button :class="{ selected: !unreadOnly }" :aria-pressed="!unreadOnly" @click="unreadOnly = false">全部记录</button>
      <button :class="{ selected: unreadOnly }" :aria-pressed="unreadOnly" @click="unreadOnly = true">只看未读</button>
    </div>
    <p v-if="!scopes.length" class="inbox-empty">{{ !history ? '选择一个连接后查看该来源的通知。' : unreadOnly ? '保留的通知都已读，已清理的错误也已确认。' : '此连接还没有本地通知记录。' }}</p>
    <section v-for="scope in scopes" :key="scopeKey(scope)" class="inbox-scope">
      <div class="inbox-scope-heading">
        <div>
          <strong>{{ sourceLabel(scope) }}</strong>
          <small>{{ scope.baseUrl }}</small>
          <small>{{ scope.accountId ? `账号：${scope.accountId}` : '实例级记录，未绑定游戏账号' }} · 历史来源</small>
        </div>
        <button class="text-button" :disabled="!store.native || store.busy || !scope.unreadCount" @click="markScopeRead(scope)">保留记录设为已读</button>
      </div>
      <p v-if="scope.truncated" class="inbox-retention">较早记录已按本机容量上限清理；当前列表仅包含保留的记录。</p>
      <p v-if="scope.quietPending > 0" class="inbox-quiet">勿扰期间留有 {{ scope.quietPending }} 条未读提醒，恢复该来源后仅显示一条摘要。</p>
      <div v-if="errorSummary(scope)" class="inbox-error-summary" role="status">
        <strong>{{ errorSummary(scope)?.unreadCount }} 条未读错误正文已清理</strong>
        <p>最近错误：{{ formatTime(errorSummary(scope)?.latestAt) }}。仅保留数量与时间，无法恢复已清理的正文。</p>
        <div v-if="pendingConfirmation === confirmationKey(scope)" class="inbox-error-confirmation" role="group" aria-label="确认已知晓已清理的错误">
          <p>确认已知晓以上 {{ errorSummary(scope)?.unreadCount }} 条错误？此操作不会把保留记录设为已读。</p>
          <button class="text-button" :disabled="!store.native || store.busy" @click="acknowledgeErrors(scope)">确认已知晓</button>
          <button class="text-button" :disabled="store.busy" @click="pendingConfirmation = ''">取消</button>
        </div>
        <button v-else class="text-button" :disabled="!store.native || store.busy" @click="prepareAcknowledgement(scope)">确认已知晓这些错误</button>
      </div>
      <article v-for="entry in visibleEntries(scope)" :key="entry.id" :class="{ unread: !entry.readAt }">
        <span class="status-dot" :class="entry.kind === 'error' ? 'auth_failed' : 'online'" />
        <div class="inbox-message"><p>{{ entry.message }}</p><small>{{ formatTime(entry.createdAt) }} · {{ entry.readAt ? '已读' : '未读' }}</small></div>
        <button v-if="!entry.readAt" class="text-button" :disabled="!store.native || store.busy" @click="store.markNotificationsRead(scope, [entry.id])">标为已读</button>
      </article>
      <button v-if="visibleEntries(scope).length < entries(scope).length" class="text-button inbox-more" @click="showMore(scope)">查看更多记录</button>
    </section>
  </section>
</template>

<style scoped>
/* 复用控制台的浅绿卡片语汇，长来源和正文在窄窗口内自然换行。 */
.notification-inbox h3{display:flex;align-items:center;gap:8px}
.inbox-description,.inbox-empty,.inbox-retention,.inbox-quiet{font-size:11px;line-height:1.9;color:#869477}
.inbox-filter{display:flex;gap:5px;margin:16px 0}
.inbox-filter button{padding:7px 12px;border:1px solid #e3e9da;border-radius:6px;background:transparent;color:#7b8b6b;font-size:11px}
.inbox-filter button.selected{background:#edf3e5;color:#557348;border-color:#c9d7bb}
.inbox-scope{border-top:1px solid #e3e9da;margin-top:14px;padding-top:16px}
.inbox-scope-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}
.inbox-scope-heading>div{min-width:0}
.inbox-scope-heading strong{font-size:12px;color:#607a54}
.inbox-scope-heading small,.inbox-message small{display:block;color:#98a58b;font-size:10px;margin-top:5px;overflow-wrap:anywhere}
.inbox-scope .text-button{flex-shrink:0;font-size:11px}
.inbox-retention,.inbox-quiet{margin-top:12px}
/* 裁剪错误单独呈现与确认，不混入保留正文的已读按钮或普通勿扰数量。 */
.inbox-error-summary{margin-top:14px;padding:12px 14px;border:1px solid #ead5c5;border-radius:8px;background:#faf3e9;color:#93674e}
.inbox-error-summary strong{font-size:12px;font-weight:500}
.inbox-error-summary p{font-size:11px;line-height:1.9;margin:6px 0}
.inbox-error-summary .text-button{color:#93674e}
.inbox-error-confirmation .text-button+.text-button{margin-left:16px}
.inbox-scope article{display:flex;align-items:flex-start;gap:10px;border-top:1px solid #edf0e4;padding:13px 0}
.inbox-scope article:first-of-type{margin-top:13px}
.inbox-scope article>.status-dot{margin-top:7px;flex-shrink:0;opacity:.4}
.inbox-scope article.unread>.status-dot{opacity:1}
.inbox-message{flex:1;min-width:0}
.inbox-message p{font-size:12px;color:#738668;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}
.inbox-more{margin-top:8px}
@media(max-width:640px){.inbox-scope-heading{align-items:flex-start;flex-direction:column;gap:9px}.inbox-scope article{flex-wrap:wrap}.inbox-message{flex-basis:70%}.inbox-scope article>.text-button{margin-left:20px}}
</style>
