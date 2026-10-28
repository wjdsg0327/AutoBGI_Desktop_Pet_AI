<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 目标历史使用独立能力与宿主状态，页面不发请求，也不据此改写当前队首。
const store = useAppStore()
const supported = computed(() => store.state?.capabilities?.sourceMode === 'pet-v1'
  && store.state.capabilities.targetsMetEvents === true)
const history = computed(() => store.state?.targetsMet)
const sameSource = computed(() => !!history.value
  && history.value.profileId === store.state?.activeProfileId
  && history.value.instanceId === store.state?.capabilities?.instanceId)
const events = computed(() => sameSource.value ? [...(history.value?.events ?? [])].reverse() : [])
const stale = computed(() => !sameSource.value || history.value?.stale || store.status !== 'online')
const status = computed(() => {
  if (!history.value || !sameSource.value) return '等待同步'
  if (history.value.status === 'persistence_failed') return '记录未保存'
  if (history.value.status === 'incompatible') return '暂不可读'
  return stale.value ? '历史记录 · 当前未同步' : '已同步'
})

// 观察与提交各自显示原始时间，二者都不是本次游戏完成培养的时间。
function eventText(kind: string, characterName: string) {
  return kind === 'talent.queue.targets-met'
    ? '原培养队列的全部基础等级与天赋目标已由可信面板核实。'
    : `「${characterName}」的基础等级与天赋目标已由可信面板核实。`
}
function timeText(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '时间不可用' : date.toLocaleString('zh-CN', { hour12: false })
}
</script>

<template>
  <section v-if="supported" class="target-history-section" aria-label="培养目标达标记录">
    <div class="overview-topline"><h3><AppIcon name="list" :size="15" /> 目标达标记录</h3><span class="updated-label">{{ status }}</span></div>
    <article class="card target-history-card">
      <p class="target-history-note">此处记录可信基础面板已经达到保存目标的事实，不表示本次游戏运行完成培养，也不表示材料库存已核验。首次连接和恢复同步只补历史。</p>
      <p v-if="history?.message" class="target-history-warning">{{ history.message }}</p>
      <p v-if="sameSource && history?.truncated" class="target-history-warning">较早记录已超出本地历史保留范围，以下仅展示最近记录。</p>
      <ol v-if="events.length" class="target-history-list" :class="{ 'is-stale': stale }">
        <li v-for="event in events" :key="event.businessEventId">
          <strong>{{ event.kind === 'talent.queue.targets-met' ? '原队列目标已达标' : '角色目标已达标' }}</strong>
          <p>{{ eventText(event.kind, event.characterName) }}</p>
          <dl class="target-event-times">
            <div><dt>面板观察</dt><dd><time :datetime="event.observedAt">{{ timeText(event.observedAt) }}</time></dd></div>
            <div><dt>记录提交</dt><dd><time :datetime="event.occurredAt">{{ timeText(event.occurredAt) }}</time></dd></div>
          </dl>
          <div class="target-event-context"><span>{{ event.configName }}</span><span>UID {{ event.role.uid }} · {{ event.role.region }}</span><span>计划版本 {{ event.planRevision }}</span></div>
          <details class="target-event-source"><summary>来源标识</summary><span>计划：{{ event.planId }}<br>执行范围：{{ event.executionContext }}<br>业务记录：{{ event.businessEventId }}</span></details>
        </li>
      </ol>
      <p v-else class="target-history-empty">{{ stale ? '目标达标记录尚未同步，连接恢复后将补取历史。' : '当前没有已核实的目标达标记录；空历史不表示培养已经完成。' }}</p>
    </article>
  </section>
</template>

<style scoped>
/* 与现有业务卡片共用颜色和间距，窄屏时间行自动换行。 */
.target-history-section{margin-top:28px;min-width:0}.overview-topline h3{display:flex;align-items:center;gap:7px}.target-history-card{min-width:0}.target-history-note{margin:0 0 15px;padding:11px 13px;border:1px solid #eae2ce;border-radius:8px;background:#f7f3e7;color:#9b8766;font-size:11px;line-height:1.9}.target-history-warning{font-size:11px;line-height:1.9;color:#ad845d;overflow-wrap:anywhere}.target-history-list{list-style:none;margin:0;padding:0;max-height:480px;overflow:auto}.target-history-list li{padding:15px 0;border-top:1px solid var(--line)}.target-history-list li:first-child{border-top:0;padding-top:2px}.target-history-list strong{font-weight:500;font-size:12px;color:#708665}.target-history-list p{font-size:12px;line-height:1.9;margin:9px 0;color:#7b8c72;overflow-wrap:anywhere}.target-event-times{display:flex;flex-wrap:wrap;gap:5px 20px;font-size:10px;line-height:1.9;margin:8px 0;color:#94a183}.target-event-times>div{display:flex;gap:8px}.target-event-times dt,.target-event-times dd{margin:0}.target-event-context{display:flex;flex-wrap:wrap;gap:5px 13px;font-size:10px;line-height:1.9;color:#a0aa94}.target-event-context span{overflow-wrap:anywhere}.target-event-source{margin-top:8px;font-size:10px;color:#a0aa94;line-height:1.9}.target-event-source summary{cursor:pointer;width:max-content}.target-event-source>span{display:block;margin-top:6px;overflow-wrap:anywhere}.target-history-empty{padding:17px;border:1px dashed #dce4d1;border-radius:8px;color:#94a183;font-size:12px;line-height:1.9}.is-stale{opacity:.75}@media(max-width:600px){.target-event-times{flex-direction:column;gap:5px}.target-history-list{max-height:420px}}
</style>
