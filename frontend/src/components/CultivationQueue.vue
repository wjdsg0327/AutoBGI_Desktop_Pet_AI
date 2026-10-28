<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api, type TaskPreset } from '../bridge'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 正式队列与旧培养配置摘要分别读取，类型以 Go 生成绑定为唯一协议来源。
const store = useAppStore()
type QueueSummary = Awaited<ReturnType<typeof api.readCultivationQueue>>
type QueueEntry = NonNullable<NonNullable<QueueSummary['result']>['plan']['entries']>[number]
const result = ref<QueueSummary | null>(null)
const presets = ref<TaskPreset[]>([])
const selectedPresetId = ref('')
const presetsLoaded = ref(false)
const presetsStale = ref(false)
const presetsLoading = ref(false)
const loading = ref(false)
const presetError = ref('')
const error = ref('')
const readFailed = ref(false)
let presetRequest = 0
let queueRequest = 0
let alive = true

// 只有明确协商的独立队列能力才能显示或请求；查询角色选择不参与此上下文。
const supported = computed(() => store.state?.capabilities?.sourceMode === 'pet-v1'
  && store.state.capabilities.cultivationQueue === true)
// 技能能力只决定下一次读取的合同；历史条目的展示始终以已接纳响应版本为准。
const skillTargetsSupported = computed(() => store.state?.capabilities?.cultivationQueueSkillTargets === true)
const online = computed(() => store.native && supported.value && store.status === 'online')
const context = computed(() => JSON.stringify([
  store.state?.activeProfileId, store.state?.connection.generation,
  store.state?.capabilities?.instanceId, supported.value, skillTargetsSupported.value,
]))
const selectedPreset = computed(() => presets.value.find(item => item.id === selectedPresetId.value))
const canRead = computed(() => online.value && !!selectedPreset.value && !presetsStale.value)
const queue = computed(() => result.value?.result)
const stale = computed(() => !!result.value && (result.value.stale || readFailed.value || !online.value || presetsStale.value
  || result.value.profileId !== store.state?.activeProfileId
  || result.value.generation !== store.state?.connection.generation
  || result.value.instanceId !== store.state?.capabilities?.instanceId
  || result.value.presetId !== selectedPresetId.value))
const statusText = computed(() => queue.value?.status === 'empty' ? '保存的队列为空'
  : queue.value?.status === 'disabled' ? '已保存 · 未启用培养意向' : '已保存 · 等待可信数据')
// 库存观察单独显示，旧服务端缺省时不推断库存，更不回填材料需求表。
const inventoryCollection = computed(() => queue.value?.inventoryCollection)
const inventoryStatus = computed(() => ({
  unavailable: '尚无独立采集', pending: '等待手动采集', observed: '已读取原始观察', expired: '采集观察已过期',
} as Record<string, string>)[inventoryCollection.value?.status || ''] || '状态未知')
function inventoryReason(value: string) { return value === 'recognition-failed' ? '识别失败' : '未返回数量' }

// 连接身份变更立即清除旧实例资料，断线或暂停则保留同一上下文的历史数据并作废在途请求。
watch(context, () => {
  presetRequest++; queueRequest++
  result.value = null; presets.value = []; selectedPresetId.value = ''
  presetsLoaded.value = false; presetsStale.value = false
  presetsLoading.value = loading.value = false
  presetError.value = error.value = ''; readFailed.value = false
}, { flush: 'sync' })
watch(() => store.status, () => {
  presetRequest++; queueRequest++
  presetsLoading.value = loading.value = false
  if (!online.value) {
    if (presetsLoaded.value) presetsStale.value = true
    if (result.value) readFailed.value = true
  }
}, { flush: 'sync' })
watch(selectedPresetId, () => {
  queueRequest++; result.value = null; loading.value = false; error.value = ''; readFailed.value = false
}, { flush: 'sync' })

// 配置列表独立读取，不复用启动面板的选择，也不猜测默认配置或用名称拼出预设 ID。
async function loadPresets() {
  if (!online.value || presetsLoading.value) return
  const version = ++presetRequest
  const expected = context.value
  const current = () => alive && version === presetRequest && expected === context.value && online.value
  presetsLoading.value = true; presetError.value = ''
  try {
    const next = await api.listPresets()
    if (!current()) return
    if (!Array.isArray(next) || next.some(item => !item.id || !item.name)) throw new Error('一条龙配置列表不完整，请重新读取。')
    presets.value = next
    presetsLoaded.value = true; presetsStale.value = false
    if (selectedPresetId.value && !selectedPreset.value) {
      if (result.value) readFailed.value = true
    }
  } catch (reason) {
    if (current()) {
      presetError.value = errorText(reason)
      if (presetsLoaded.value) presetsStale.value = true
    }
  } finally { if (current()) presetsLoading.value = false }
}

// 每次读取冻结连接、实例和预设；迟到或暂停期间的回包不能覆盖当前选择，错误保留旧缓存。
async function readQueue() {
  if (!canRead.value || loading.value) return
  const version = ++queueRequest
  const expected = context.value
  const presetId = selectedPresetId.value
  const profileId = store.state?.activeProfileId
  const generation = store.state?.connection.generation
  const instanceId = store.state?.capabilities?.instanceId
  const expectedSchema = skillTargetsSupported.value ? 2 : 1
  const current = () => alive && version === queueRequest && expected === context.value
    && presetId === selectedPresetId.value && canRead.value
  loading.value = true; error.value = ''
  try {
    const next = await api.readCultivationQueue(presetId)
    if (!current()) return
    if (next.profileId !== profileId || next.generation !== generation || next.instanceId !== instanceId
      || next.presetId !== presetId || !next.result || next.result.instanceId !== instanceId
      || next.result.presetId !== presetId || next.result.scope !== 'instance-config-queue'
      || next.result.schemaVersion !== expectedSchema
      || typeof next.result.plan?.revision !== 'string' || !/^(0|[1-9]\d*)$/.test(next.result.plan.revision)
      || !Array.isArray(next.result.plan.entries)
      || expectedSchema === 2 && next.result.plan.entries.some(entry => !savedSkillIdentityValid(entry))) {
      throw new Error('队列响应与当前连接或配置不一致，请重新读取。')
    }
    result.value = next
    readFailed.value = false
  } catch (reason) {
    if (current()) {
      error.value = errorText(reason)
      if (result.value) readFailed.value = true
    }
  } finally { if (version === queueRequest) loading.value = false }
}

// 未知数量与时间保留未知，版本号保持十进制原文，不转为可能丢精度的 JavaScript 数字。
function errorText(reason: unknown) { return reason instanceof Error ? reason.message : '读取失败，请稍后重试。' }
function numberText(value: number | null | undefined) { return value == null ? '未知' : value.toLocaleString('zh-CN') }
function talentsText(value: number[] | null | undefined) { return value?.length === 3 ? value.join(' / ') : '未知' }
function timeText(value: string | null | undefined) { if (!value) return '未知'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString('zh-CN', { hour12: false }) }
function sourceText(value: string) { return value === 'official-calculator-cache' ? '官方计算器缓存' : value === 'panel-already-met' ? '面板缓存已达目标' : '未知' }
// 元素标签仅翻译已保存的规范名称，不能根据角色名、面板或候选资料猜测技能身份。
const savedElementNames: Record<string, string> = {
  anemo: '风', geo: '岩', electro: '雷', dendro: '草', hydro: '水', pyro: '火', cryo: '冰',
}
function savedElementText(entry: QueueEntry) { return savedElementNames[entry.targetSkills?.element || ''] || '未知' }
// Go 已验证网络合同；展示边界再拒绝无法安全往返的编号，空指针保留旧计划未绑定的事实。
function savedSkillIdentityValid(entry: QueueEntry) {
  if (!Object.hasOwn(entry, 'targetSkills')) return false
  const identity = entry.targetSkills
  return identity === null || identity !== undefined && Object.hasOwn(savedElementNames, identity.element)
    && Array.isArray(identity.skillIds) && identity.skillIds.length === 3
    && identity.skillIds.every(id => Number.isSafeInteger(id) && id > 0)
    && new Set(identity.skillIds).size === 3
}
onBeforeUnmount(() => { alive = false; presetRequest++; queueRequest++ })
</script>

<template>
  <!-- 沿用纸感卡片与松针绿，独立选择配置后展示保存事实，不提供执行或编辑入口。 -->
  <section v-if="supported" class="cultivation-queue-section" aria-label="已保存培养队列">
    <div class="overview-topline"><h3>已保存培养队列</h3><span class="subtle-badge">只读 · 独立配置</span></div>
    <article class="card cultivation-queue-card">
      <div class="queue-selector">
        <label>一条龙配置<select v-model="selectedPresetId" aria-label="培养队列的一条龙配置" :disabled="!online || presetsLoading || !presetsLoaded">
          <option value="" disabled>请选择服务端配置</option>
          <option v-if="selectedPresetId && !selectedPreset" :value="selectedPresetId" disabled>此前所选配置已不可用</option>
          <option v-for="preset in presets" :key="preset.id" :value="preset.id">{{ preset.name }}</option>
        </select></label>
        <button type="button" class="button button-light" :disabled="!online || presetsLoading" @click="loadPresets">{{ presetsLoading ? '正在读取配置…' : presetsLoaded ? '刷新配置列表' : '读取配置列表' }}</button>
        <button type="button" class="button button-light" :disabled="!canRead || loading" @click="readQueue"><AppIcon name="refresh" :size="14" />{{ loading ? '正在读取…' : result ? '刷新培养队列' : '读取培养队列' }}</button>
      </div>
      <p class="domain-caption">按一条龙配置读取正式保存的顺序和目标。身份由计划绑定，独立于收益与旧培养配置的查询角色选择。</p>
      <p class="queue-capability-note">自动执行、自动出队与完成事件尚未开放。队首表示第一个保存条目，不表示正在执行；库存未核验，已有数量和缺口保持未知。</p>
      <div v-if="presetError" class="notice error-notice" role="alert"><AppIcon name="info" /><span>{{ presetError }}</span></div>
      <div v-if="error" class="notice error-notice" role="alert"><AppIcon name="info" /><span>{{ error }}</span></div>
      <p v-if="presetsStale" class="domain-warning">配置列表为历史数据，请重新读取列表后再刷新队列。</p>
      <p v-if="presetsLoaded && selectedPresetId && !selectedPreset" class="domain-warning">此前所选配置已不在服务端列表中，请选择其他配置。</p>
      <p v-if="!online" class="domain-warning">当前连接未在线，暂停读取；上次结果仅作历史记录。</p>
      <p v-if="presetsLoaded && !presets.length && !presetsStale" class="domain-caption">服务端当前没有可选择的一条龙配置。</p>
      <template v-if="queue && result">
        <div class="queue-result-heading"><strong>{{ queue.presetName }}</strong><span class="subtle-badge">{{ statusText }}</span><b v-if="stale" class="queue-stale">历史队列 · 当前未同步</b></div>
        <dl class="queue-context">
          <div><dt>绑定身份</dt><dd>{{ queue.plan.role.uid ? `UID ${queue.plan.role.uid} · ${queue.plan.role.region}` : '未绑定 UID 与区服' }}</dd></div>
          <div><dt>计划版本</dt><dd class="identifier">{{ queue.plan.revision }}</dd></div>
          <div><dt>保存时间</dt><dd>{{ timeText(queue.plan.updatedAt) }}</dd></div>
          <div><dt>获取时间</dt><dd>{{ timeText(result.fetchedAt) }}</dd></div>
          <div><dt>读取范围</dt><dd>当前服务实例与所选一条龙配置<details class="queue-source"><summary>来源标识</summary><span class="queue-context-id identifier">实例：{{ result.instanceId }}<br>目录摘要：{{ queue.executionContext }}</span></details></dd></div>
        </dl>
        <!-- 独立库存观察不参与已有数量、缺口或队首推断，过期结果只作为历史展示。 -->
        <article v-if="inventoryCollection" class="queue-inventory" aria-label="独立库存观察">
          <div class="queue-result-heading"><strong>独立库存观察</strong><span class="subtle-badge">{{ inventoryStatus }}</span></div>
          <p class="domain-caption">原始识别数量仅供参考，不代表已核验库存；不会自动计算缺口、合成或推进培养队列。请在 AutoBGI 网页准备请求，在 BetterGI 手动采集后读取结果。</p>
          <p v-if="inventoryCollection.warning" class="domain-warning">{{ inventoryCollection.warning }}</p>
          <dl v-if="inventoryCollection.requestId" class="queue-context">
            <div><dt>请求创建 / 截止</dt><dd>{{ timeText(inventoryCollection.createdAt) }}<br>{{ timeText(inventoryCollection.expiresAt) }}</dd></div>
            <div><dt>采集开始 / 结束</dt><dd>{{ timeText(inventoryCollection.sourceTime) }}<br>{{ timeText(inventoryCollection.finishedAt) }}</dd></div>
          </dl>
          <details v-if="inventoryCollection.materials?.length" class="queue-source"><summary>查看 {{ inventoryCollection.materials.length }} 项原始库存<span v-if="inventoryCollection.status === 'expired'"> · 已过期，仅作历史参考</span></summary>
            <div class="domain-table-scroll"><table class="domain-table"><thead><tr><th>材料</th><th>观察数量</th><th>识别状态</th></tr></thead><tbody><tr v-for="material in inventoryCollection.materials" :key="material.materialId"><td>{{ material.name }}</td><td>{{ material.known ? numberText(material.count) : '未知' }}</td><td>{{ material.known ? '已返回数量' : inventoryReason(material.reason) }}</td></tr></tbody></table></div>
          </details>
        </article>
        <!-- 双层表格分别保留手机横向滚动，不用库存空值代替零或完成结论。 -->
        <div v-if="queue.plan.entries?.length" class="domain-table-scroll queue-table-scroll">
          <table class="domain-table queue-table"><thead><tr><th>顺序 / 角色</th><th>当前缓存</th><th>明确目标</th><th>缓存详情</th></tr></thead><tbody>
            <tr v-for="(entry, index) in queue.plan.entries" :key="entry.id">
              <td><span class="queue-entry-order">{{ index + 1 }}</span><strong>{{ entry.name }}</strong><span v-if="entry.id === queue.currentEntryId" class="queue-front">队首</span><small class="queue-subline">{{ entry.characterId }}</small></td>
              <td>Lv.{{ numberText(entry.currentLevel) }}<small class="queue-subline">A / E / Q：{{ talentsText(entry.currentTalents) }}</small></td>
              <td>Lv.{{ numberText(entry.targetLevel) }}<small class="queue-subline">A / E / Q：{{ talentsText(entry.targetTalents) }}</small>
                <!-- 已保存技能与目标等级放在同格，A/E/Q 保留原次序且不把编号当作技能组或技能名。 -->
                <div v-if="queue.schemaVersion === 2 && entry.targetSkills" class="queue-saved-skills" aria-label="已保存技能绑定">
                  <span class="queue-skill-element">已保存绑定 · {{ savedElementText(entry) }}元素</span>
                  <span class="queue-skill-ids"><span v-for="(skillId, slot) in entry.targetSkills.skillIds" :key="slot" class="identifier">{{ ['A', 'E', 'Q'][slot] }} #{{ skillId }}</span></span>
                </div>
                <small v-else class="queue-subline queue-skill-unknown">{{ queue.schemaVersion === 2 ? '旧计划尚未绑定技能' : '技能绑定未知 · 旧服务未提供' }}</small>
              </td>
              <td><details class="queue-detail"><summary>查看需求与来源</summary>
                <p>加入时间：{{ timeText(entry.addedAt) }}<br>面板时间：{{ timeText(entry.panelUpdatedAt) }}</p>
                <p>需求来源：{{ sourceText(entry.requirementSource) }}<br>源时间：{{ timeText(entry.requirementsSourceTime) }}<br>获取时间：{{ timeText(entry.requirementsFetchedAt) }}</p>
                <details class="queue-source"><summary>来源标识</summary><span class="queue-context-id identifier">{{ entry.requirementSource || '未知' }}<br>{{ entry.requestFingerprint || '未提供请求指纹' }}</span></details>
                <p v-if="entry.warning" class="domain-warning">{{ entry.warning }}</p>
                <template v-if="entry.requirementsAvailable">
                  <div v-if="entry.materials?.length" class="domain-table-scroll"><table class="domain-table"><thead><tr><th>材料</th><th>需要</th><th>已有</th><th>还缺</th></tr></thead><tbody><tr v-for="material in entry.materials" :key="material.materialId"><td>{{ material.name }}<small v-if="material.excluded" class="queue-subline">不参与完成条件</small></td><td>{{ numberText(material.required) }}</td><td>{{ numberText(material.owned) }}</td><td>{{ numberText(material.missing) }}</td></tr></tbody></table></div>
                  <p v-else>此缓存没有材料需求项；不据此认定队列完成。</p>
                </template><p v-else>该目标的需求缓存不可用；不会改用其他目标或其他身份的缓存。</p>
              </details></td>
            </tr>
          </tbody></table>
        </div>
        <p v-else class="queue-empty">此配置保存的培养队列为空。请在 AutoBGI 配置页添加培养条目。</p>
        <p v-for="warning in queue.warnings" :key="warning" class="domain-warning">{{ warning }}</p>
      </template>
      <p v-else-if="!loading" class="domain-caption">先读取配置列表，再选择一条龙并读取队列；不会自动选取默认配置或修改计划。</p>
    </article>
  </section>
</template>

<style scoped>
/* 技能身份复用松针绿与纸感底色，长编号在目标单元格内换行，外层手机横向滚动保持不变。 */
.queue-saved-skills{display:flex;flex-direction:column;align-items:flex-start;gap:5px;margin-top:8px;max-width:240px;white-space:normal}.queue-skill-element{border:1px solid #dce6d3;border-radius:4px;padding:2px 5px;background:#f0f5e9;color:#7b906e;font-size:9px;line-height:1.7}.queue-skill-ids{display:flex;flex-wrap:wrap;gap:3px 8px;color:#89977c;font-size:9px;line-height:1.8;overflow-wrap:anywhere}.queue-skill-unknown{max-width:230px;white-space:normal}
/* 观察区复用当前浅色卡片，不将其数量混入培养需求。 */
.queue-inventory{margin:14px 0;padding:12px 14px;border:1px solid #e6e5d8;border-radius:9px;background:#fcfbf4}.queue-inventory .queue-result-heading{margin:0 0 9px}.queue-inventory .queue-context{margin:12px 0 0}.queue-inventory .domain-table-scroll{max-height:320px;margin-top:10px}
/* 复用现有颜色和宽表语言，长配置名、身份和版本可换行，窄屏滚动限制在表格内。 */
.cultivation-queue-section{margin-top:28px;min-width:0}.cultivation-queue-card{min-width:0}.queue-selector{display:flex;flex-wrap:wrap;align-items:flex-end;gap:9px}.queue-selector label{display:flex;flex:1;min-width:190px;flex-direction:column;gap:7px;font-size:10px;color:#819279}.queue-selector select{width:100%;min-width:0;padding:10px;border:1px solid var(--line);border-radius:7px;background:#fbfcf5}.queue-selector .button{font-size:10px;white-space:nowrap}.queue-capability-note{padding:11px 13px;margin:12px 0 16px;background:#f7f3e7;border:1px solid #eae2ce;border-radius:8px;color:#9b8766;font-size:10px;line-height:1.9}.queue-result-heading{display:flex;flex-wrap:wrap;align-items:center;gap:10px;margin:17px 0 12px}.queue-result-heading>strong{font-weight:500;font-size:13px;overflow-wrap:anywhere}.queue-stale{font-size:10px;color:#b17e57;font-weight:500}.queue-context{display:grid;grid-template-columns:1fr 1fr;gap:12px 20px;margin:0 0 18px;font-size:10px;line-height:1.8}.queue-context>div:last-child{grid-column:1/-1}.queue-context dt{color:#a0aa94}.queue-context dd{margin:4px 0 0;color:#778970;overflow-wrap:anywhere}.queue-context-id{display:block;font-size:9px;color:#a0aa94;margin-top:3px;overflow-wrap:anywhere}.queue-source{margin-top:5px;font-size:9px;color:#94a087}.queue-source summary{cursor:pointer}.queue-table-scroll{max-height:none}.queue-table{min-width:630px}.queue-table td{vertical-align:top}.queue-table strong{font-weight:500;color:#6f8366}.queue-entry-order{display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;border-radius:50%;background:#eaf0e0;margin-right:7px;font-size:9px}.queue-front{font-size:9px;color:#a08a60;border:1px solid #e9dfc8;border-radius:4px;padding:2px 4px;margin-left:7px}.queue-subline{display:block;font-size:9px;line-height:1.8;color:#a0ab93;margin-top:5px}.queue-detail{width:235px;white-space:normal}.queue-detail summary{cursor:pointer;color:#708b64;line-height:1.8}.queue-detail p{font-size:9px;line-height:1.9;margin:10px 0;color:#9aa48e;overflow-wrap:anywhere}.queue-detail .domain-table-scroll{margin-top:12px;max-height:250px}.queue-detail .domain-table td,.queue-detail .domain-table th{padding:8px}.queue-empty{font-size:11px;line-height:1.9;padding:18px;border:1px dashed #dce4d1;border-radius:8px;color:#94a183}@media(max-width:600px){.queue-context{grid-template-columns:1fr}.queue-selector label{flex-basis:100%;min-width:0}.queue-selector .button{flex:1}.queue-context>div:last-child{grid-column:auto}}
</style>
