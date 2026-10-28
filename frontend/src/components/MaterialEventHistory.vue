<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 历史只消费宿主已持久接收的结构化状态，不在页面启动请求或根据配置名修改队列。
const store = useAppStore()
const supported = computed(() => store.state?.capabilities?.sourceMode === 'pet-v1'
  && store.state.capabilities.materialsReadyEvents === true)
const history = computed(() => store.state?.materialsReady)
const sameSource = computed(() => !!history.value
  && history.value.profileId === store.state?.activeProfileId
  && history.value.instanceId === store.state?.capabilities?.instanceId)
const events = computed(() => sameSource.value ? [...(history.value?.events ?? [])].reverse() : [])
const stale = computed(() => !sameSource.value || history.value?.stale || store.status !== 'online')
const status = computed(() => {
  if (!history.value || !sameSource.value) return '等待同步'
  if (history.value.status === 'persistence_failed') return '记录未保存'
  if (history.value.status === 'incompatible') return '暂不可读'
  if (stale.value) return '历史记录 · 当前未同步'
  return '已同步'
})

// 文案只描述材料条件，未知游戏进度不能从此记录推导为升级或加点完成。
function eventText(kind: string, characterName: string) {
  return kind === 'talent.queue.materials-ready'
    ? '本次培养队列的材料条件均已满足，可以添加新目标。'
    : `「${characterName}」的培养材料条件已满足。`
}
function timeText(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '源时间不可用' : date.toLocaleString('zh-CN', { hour12: false })
}
</script>

<template>
  <section v-if="supported" class="material-history-section" aria-label="培养材料条件记录">
    <div class="overview-topline"><h3><AppIcon name="list" :size="15" /> 材料条件记录</h3><span class="updated-label">{{ status }}</span></div>
    <article class="card material-history-card">
      <p class="material-history-note">此处记录服务端确认的材料条件，不代表游戏内升级或天赋加点完成。首次连接、恢复同步与历史补偿只补记录，不逐条弹出提醒。</p>
      <p v-if="history?.message" class="material-history-warning">{{ history.message }}</p>
      <p v-if="sameSource && history?.truncated" class="material-history-warning">较早记录已超出历史保留范围，仅展示本地保存的最近记录。</p>
      <ol v-if="events.length" class="material-history-list" :class="{ 'is-stale': stale }">
        <li v-for="event in events" :key="event.businessEventId">
          <div class="material-event-heading"><strong>{{ event.kind === 'talent.queue.materials-ready' ? '队列材料条件满足' : '角色材料条件满足' }}</strong><time :datetime="event.occurredAt">{{ timeText(event.occurredAt) }}</time></div>
          <p>{{ eventText(event.kind, event.characterName) }}</p>
          <div class="material-event-context"><span>{{ event.configName }}</span><span>UID {{ event.role.uid }} · {{ event.role.region }}</span><span>计划版本 {{ event.planRevision }}</span></div>
          <details class="material-event-source"><summary>来源标识</summary><span>计划：{{ event.planId }}<br>执行范围：{{ event.executionContext }}<br>业务记录：{{ event.businessEventId }}</span></details>
        </li>
      </ol>
      <p v-else class="material-history-empty">{{ stale ? '材料条件记录尚未同步，连接恢复后将补取历史。' : '当前没有材料条件满足记录；空历史不表示培养已经完成。' }}</p>
    </article>
  </section>
</template>

<style scoped>
.material-history-section{margin-top:28px;min-width:0}.overview-topline h3{display:flex;align-items:center;gap:7px}.material-history-card{min-width:0}.material-history-note{margin:0 0 15px;padding:11px 13px;border:1px solid #eae2ce;border-radius:8px;background:#f7f3e7;color:#9b8766;font-size:11px;line-height:1.9}.material-history-warning{font-size:11px;line-height:1.9;color:#ad845d;overflow-wrap:anywhere}.material-history-list{list-style:none;margin:0;padding:0;max-height:480px;overflow:auto}.material-history-list li{padding:15px 0;border-top:1px solid var(--line)}.material-history-list li:first-child{border-top:0;padding-top:2px}.material-event-heading{display:flex;flex-wrap:wrap;justify-content:space-between;gap:7px}.material-event-heading strong{font-weight:500;font-size:12px;color:#708665}.material-event-heading time{font-size:10px;color:#a0aa94}.material-history-list p{font-size:12px;line-height:1.9;margin:9px 0;color:#7b8c72;overflow-wrap:anywhere}.material-event-context{display:flex;flex-wrap:wrap;gap:5px 13px;font-size:10px;line-height:1.9;color:#a0aa94}.material-event-context span{overflow-wrap:anywhere}.material-event-source{margin-top:8px;font-size:10px;color:#a0aa94;line-height:1.9}.material-event-source summary{cursor:pointer;width:max-content}.material-event-source>span{display:block;margin-top:6px;overflow-wrap:anywhere}.material-history-empty{padding:17px;border:1px dashed #dce4d1;border-radius:8px;color:#94a183;font-size:12px;line-height:1.9}.is-stale{opacity:.75}@media(max-width:600px){.material-event-heading{flex-direction:column;gap:5px}.material-history-list{max-height:420px}}
</style>
