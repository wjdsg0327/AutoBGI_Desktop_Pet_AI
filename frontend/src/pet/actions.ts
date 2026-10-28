import type { AppState, NotificationErrorSummary, NotificationHistoryScope, NotificationRecord } from '../../bindings/autobgi-pet/internal/service/models'
import { parseNotificationTime } from './actionTime'

// 动画只读取当前事实；命令提交成功没有可信的任务完成含义，因此不提供 success 输入。
export type PetAction = 'dragging' | 'sleep' | 'warning' | 'offline' | 'working' | 'wave' | 'idle'
export type PetActionFacts = {
  dragging: boolean
  sleeping: boolean
  unreadError: boolean
  status: string
  taskRunning: boolean
  wave: boolean
}

// 正文与裁剪摘要共用完整来源选择，避免两种错误提示使用不同的实例或账号范围。
function currentNotificationScope(state: AppState | null): NotificationHistoryScope | null {
  if (!state || !state.activeProfileId || state.connection.profileId !== state.activeProfileId) return null
  const profile = state.profiles?.find(item => item.id === state.activeProfileId)
  const history = state.notificationHistory
  if (!profile || !history || history.profileId !== profile.id || history.baseUrl !== profile.baseUrl) return null

  // 与服务端 notificationContextLocked 一致：探测前不伪造实例，仅保留明确的 Legacy 只读回退。
  let sourceMode = 'unverified'
  let instanceId = ''
  const capabilities = state.capabilities
  if (capabilities?.sourceMode === 'pet-v1' && capabilities.instanceId) {
    sourceMode = 'pet-v1'
    instanceId = capabilities.instanceId
  } else if (capabilities?.sourceMode === 'legacy' ||
    !capabilities && state.snapshot?.profileId === profile.id && state.snapshot.sourceMode === 'legacy') {
    sourceMode = 'legacy'
  }

  return (history.scopes || []).find(scope => scope.profileId === profile.id && scope.baseUrl === profile.baseUrl &&
    scope.sourceMode === sourceMode && scope.instanceId === instanceId && scope.accountId === null) || null
}

// 只识别正文中的明确 error 和显式未读状态，不由总数量、气泡内容或命令回执猜测错误。
export function currentUnreadError(state: AppState | null): NotificationRecord | null {
  const entries = currentNotificationScope(state)?.entries || []
  for (let index = entries.length - 1; index >= 0; index--) {
    const entry = entries[index]!
    if (entry.kind === 'error' && entry.readAt === null) return entry
  }
  return null
}

// 摘要是独立计数事实，严格校验整数和十进制版本；版本不经过 Number，避免 uint64 精度丢失。
export function notificationErrorSummary(scope: NotificationHistoryScope): NotificationErrorSummary | null {
  const value = scope.errorSummary
  if (!value || typeof value !== 'object' || scope.truncated !== true ||
    Object.keys(value).length !== 4 || !['revision', 'unreadCount', 'quietPending', 'latestAt'].every(key => Object.hasOwn(value, key))) return null
  const revision = value.revision
  if (typeof revision !== 'string' || !/^[1-9][0-9]{0,19}(?![\s\S])/.test(revision) ||
    revision.length === 20 && revision > '18446744073709551615') return null
  const uint32 = (count: unknown): count is number => typeof count === 'number' && Number.isInteger(count) && count >= 0 && count <= 4294967295
  if (!uint32(value.unreadCount) || !uint32(value.quietPending) || !uint32(scope.quietPending) ||
    value.quietPending > value.unreadCount || value.quietPending > scope.quietPending) return null
  if (value.unreadCount === 0) return value.quietPending === 0 && value.latestAt === null ? value : null
  return parseNotificationTime(value.latestAt) ? value : null
}

// 只有当前完整来源的有效未读裁剪摘要延续 warning，空摘要和旧实例都不能伪装成一条通知正文。
export function currentUnreadErrorSummary(state: AppState | null): NotificationErrorSummary | null {
  const scope = currentNotificationScope(state)
  const summary = scope ? notificationErrorSummary(scope) : null
  return summary && summary.unreadCount > 0 ? summary : null
}

// 每次重算都读取最新事实，高优先级不会被挥手覆盖；已读和松开拖动后自然回到当前状态。
export function selectPetAction(facts: PetActionFacts): PetAction {
  if (facts.dragging) return 'dragging'
  if (facts.sleeping) return 'sleep'
  if (facts.unreadError) return 'warning'
  if (facts.status !== 'online') return 'offline'
  if (facts.taskRunning) return 'working'
  return facts.wave ? 'wave' : 'idle'
}
