<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '../bridge'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 业务摘要按需读取一次，不让大背包进入所有窗口的高频状态广播。
const store = useAppStore()
type DomainResult = Awaited<ReturnType<typeof api.readDomain>>
const result = ref<DomainResult | null>(null)
// 角色和扫描列表独立保留，切换角色清除摘要时仍能选择另一个真实选项。
const queryRoles = ref<NonNullable<DomainResult['queryRoles']> | null>(null)
const scanAccounts = ref<NonNullable<DomainResult['scanAccounts']> | null>(null)
const selectedQueryRoleId = ref('')
const queryRolesStale = ref(false)
const selected = ref('')
const filter = ref('')
const loading = ref(false)
const error = ref('')
let requestVersion = 0
const supported = computed(() => store.state?.capabilities?.sourceMode === 'pet-v1'
  && (store.state.capabilities.earnings || store.state.capabilities.backpack || store.state.capabilities.cultivationPlans || store.state.capabilities.queryRoles))
const supportsQueryRoles = computed(() => store.state?.capabilities?.sourceMode === 'pet-v1' && store.state.capabilities.queryRoles === true)
const queryRoleItems = computed(() => queryRoles.value?.items || [])
const missingQueryRole = computed(() => !!selectedQueryRoleId.value && !queryRoleItems.value.some(role => role.queryRoleId === selectedQueryRoleId.value))
const stale = computed(() => result.value?.stale || store.status !== 'online'
  || result.value?.profileId !== store.state?.activeProfileId
  || result.value?.generation !== store.state?.connection.generation
  || result.value?.instanceId !== store.state?.capabilities?.instanceId)
const items = computed(() => (result.value?.backpack?.items || [])
  .filter(item => !filter.value.trim() || item.name.includes(filter.value.trim()) || item.category.includes(filter.value.trim())))
const scopeLabel = computed(() => scanAccounts.value?.items?.find(item => item.scanAccountId === result.value?.backpack?.selectedScanAccountId)?.label || '扫描档案')

// 连接身份、脱敏资料或完整能力改变时清除查询上下文，等价心跳不影响手动读取结果。
watch(() => store.captureReadIdentity(), () => {
  requestVersion++; result.value = null; queryRoles.value = null; scanAccounts.value = null
  selectedQueryRoleId.value = ''; queryRolesStale.value = false; selected.value = ''; filter.value = ''; error.value = ''; loading.value = false
}, { flush: 'sync' })
// 暂停或断线使在途读取立即失效；恢复在线仍保留历史标记，直到用户重新取得有效摘要。
watch(() => store.captureReadContext(), () => {
  requestVersion++; loading.value = false
  if (result.value) result.value.stale = true
  if (queryRoles.value) queryRolesStale.value = true
}, { flush: 'sync' })
// 角色改变立即清除前角色数据；选择器保留，允许新选择使正在等待的旧请求失效。
async function changeQueryRole() {
  requestVersion++; result.value = null; error.value = ''; loading.value = false
  if (!selectedQueryRoleId.value || missingQueryRole.value) {
    error.value = '所选查询角色不在缓存列表中，请重新选择；不会自动改用默认角色。'
    return
  }
  await read()
}
async function read() {
  if (store.status !== 'online' || !supported.value) return
  const version = ++requestVersion
  const generation = store.state?.connection.generation
  const profileId = store.state?.activeProfileId
  const instanceId = store.state?.capabilities?.instanceId
  const context = store.captureReadContext()
  const roleMode = supportsQueryRoles.value
  const requestedRoleId = roleMode ? selectedQueryRoleId.value : ''
  const sameContext = () => store.isReadContextCurrent(context) && version === requestVersion && generation === store.state?.connection.generation
    && profileId === store.state?.activeProfileId && instanceId === store.state?.capabilities?.instanceId
    && roleMode === supportsQueryRoles.value
  loading.value = true; error.value = ''
  try {
    const next = await api.readDomain(selected.value, requestedRoleId)
    if (!sameContext() || next.generation !== generation || next.profileId !== profileId || next.instanceId !== instanceId) return
    // 服务已校验的列表与角色解析错误分别处理，无默认或成员消失时仍允许从最新列表重新选择。
    if (roleMode) {
      const freshRoles = next.queryRoles
      if (freshRoles) { queryRoles.value = freshRoles; queryRolesStale.value = false }
      else queryRolesStale.value = true
      const actualRoleId = next.queryRoleId
      const knownRole = !!actualRoleId && freshRoles?.items?.some(role => role.queryRoleId === actualRoleId)
      // 列表可用并不代表身份解析成功，角色块仍须通过解析结果、成员与显式选择的一致性检查。
      if (!freshRoles || next.errors?.queryRoles || !knownRole || (requestedRoleId && actualRoleId !== requestedRoleId)) {
        next.earnings = null; next.cultivationPlans = null; next.queryRoleId = null
        error.value = !freshRoles ? '查询角色缓存暂不可用，收益与培养面板未展示；扫描资料仍可独立读取。'
          : '查询角色未能与真实缓存列表对应，请重新选择；不会自动改用默认角色。'
      } else if (!requestedRoleId) selectedQueryRoleId.value = actualRoleId!
    }
    if (next.scanAccounts) scanAccounts.value = next.scanAccounts
    result.value = next
    // 根目录 ID 允许为空字符串；只有 null 才表示服务端没有默认扫描档案。
    if (next.backpack) selected.value = next.backpack.selectedScanAccountId
    else if (next.scanAccounts?.defaultScanAccountId != null) selected.value = next.scanAccounts.defaultScanAccountId
  } catch (reason) {
    if (sameContext()) {
      error.value = reason instanceof Error ? reason.message : '业务摘要暂不可用'
      if (result.value) result.value.stale = true
      if (queryRoles.value) queryRolesStale.value = true
    }
  } finally { if (version === requestVersion) loading.value = false }
}
function number(value: number | null | undefined) { return value == null ? '未知' : value.toLocaleString('zh-CN') }
function time(value: string | null | undefined) { if (!value) return '来源未提供时间'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '来源时间不可用' : date.toLocaleString('zh-CN', { hour12: false }) }
function errorLabel(key: string) { return ({ queryRoles: '查询角色', scanAccounts: '扫描档案', earnings: '收益', backpack: '背包', cultivationPlans: '培养计划' })[key] || '业务数据' }
</script>

<template>
  <section class="domain-section" aria-label="业务摘要">
    <div class="overview-topline"><h3>已有业务数据</h3><button class="text-button" :disabled="!supported || store.status !== 'online' || loading" @click="read()"><AppIcon name="refresh" :size="14" />{{ loading ? '正在读取…' : result ? '刷新业务摘要' : '读取业务摘要' }}</button></div>
    <div v-if="!supported" class="notice"><AppIcon name="info" /><span>当前连接未协商查询角色、收益、背包或培养计划能力。更新 AutoBGI 服务端后重新连接可检测。</span></div>
    <div v-if="error" class="notice error-notice" role="alert"><AppIcon name="info" /><span>{{ error }}</span></div>
    <!-- 查询角色只来自手动读取的缓存列表，不提供登录切换或执行账号控制。 -->
    <div v-if="supportsQueryRoles && queryRoles" class="domain-query-role">
      <div class="domain-context"><label>查询角色<select v-model="selectedQueryRoleId" aria-label="查询角色" :disabled="store.status !== 'online' || !queryRoleItems.length" @change="changeQueryRole()"><option v-if="!selectedQueryRoleId" value="" disabled>请选择查询角色</option><option v-if="missingQueryRole" :value="selectedQueryRoleId" disabled>此前所选角色已不在缓存列表</option><option v-for="role in queryRoleItems" :key="role.queryRoleId" :value="role.queryRoleId">{{ role.nickname || '未命名角色' }} · UID {{ role.uid }} · {{ role.region }}</option></select></label><b v-if="queryRolesStale">角色列表为历史缓存</b></div>
      <p class="domain-caption">仅改变收益与培养面板的读取角色；不会切换游戏账号，也不会改变一条龙或实例任务。</p>
      <p class="domain-caption">来源：米游社绑定缓存（{{ queryRoles.source }}） · {{ time(queryRoles.updatedAt) }}<br>缓存列表不能证明当前游戏登录身份。<span v-if="!queryRoleItems.length">缓存中暂无可查询角色。</span></p>
      <p v-for="warning in queryRoles.warnings" :key="warning" class="domain-warning">{{ warning }}</p>
    </div>
    <div v-if="result || scanAccounts?.items?.length" class="domain-context"><span v-if="result">读取于 {{ time(result.fetchedAt) }}</span><b v-if="result && stale">历史摘要 · 当前未同步</b><label v-if="scanAccounts?.items?.length">背包扫描档案<select v-model="selected" aria-label="背包扫描档案" :disabled="loading || store.status !== 'online'" @change="read()"><option v-for="account in scanAccounts.items" :key="account.scanAccountId" :value="account.scanAccountId">{{ account.label }}</option></select></label></div>
    <p v-if="result || queryRoles" class="domain-caption">{{ supportsQueryRoles ? '扫描档案是独立资料，档案名称不代表查询角色 UID；角色模式的培养材料已有数量与缺口保持未知。' : '扫描档案仅用于查看已有背包资料；不会切换游戏账号，也不会改变一条龙执行目标。' }}</p>
    <template v-if="result">
      <div v-for="(message, key) in result.errors" :key="key" class="notice error-notice"><AppIcon name="info" /><span>{{ errorLabel(String(key)) }}：{{ message }}</span></div>
      <!-- 收益按服务端日期和角色标注，缺记录时维持未知，绝不填零。 -->
      <article v-if="result.earnings" class="card domain-card">
        <div class="card-heading"><h3>摩拉收益 · {{ result.earnings.date }}</h3><span class="subtle-badge">{{ result.earnings.timezone }}</span></div>
        <div class="earnings-total">{{ number(result.earnings.total) }}<small>摩拉</small></div>
        <p class="domain-caption">{{ result.earnings.currentRole.nickname || '当前配置角色' }} · UID {{ result.earnings.currentRole.uid || '未知' }} · {{ result.earnings.currentRole.region || '区服未知' }}</p>
        <dl class="earnings-parts"><div><dt>精英</dt><dd>{{ number(result.earnings.elite) }}</dd></div><div><dt>小怪</dt><dd>{{ number(result.earnings.small) }}</dd></div><div><dt>宝箱</dt><dd>{{ number(result.earnings.chest) }}</dd></div></dl>
        <p class="domain-caption">来源：{{ result.earnings.source }} · {{ time(result.earnings.updatedAt) }}<br>按来源日历日期统计；{{ result.earnings.resetTime || '来源未提供游戏重置时间' }}</p>
        <p v-for="warning in result.earnings.warnings" :key="warning" class="domain-warning">{{ warning }}</p>
      </article>
      <!-- 长背包可筛选并限制一次渲染数量，完整计数仍显示真实识别条目数。 -->
      <article v-if="result.backpack" class="card domain-card">
        <div class="card-heading"><h3>背包 · {{ scopeLabel }}</h3><span class="subtle-badge">扫描识别数据</span></div>
        <p class="domain-caption">扫描于 {{ time(result.backpack.scannedAt) }} · {{ result.backpack.timezone }}<br>仅包含成功识别的条目；未出现的物品数量未知。</p>
        <input v-model="filter" class="domain-filter" aria-label="筛选背包物品" placeholder="按物品名称或分类筛选" maxlength="100" />
        <div class="domain-table-scroll"><table class="domain-table"><thead><tr><th>物品</th><th>分类</th><th>已识别数量</th></tr></thead><tbody><tr v-for="item in items.slice(0, 200)" :key="item.id"><td>{{ item.name }}</td><td>{{ item.category || '未分类' }}</td><td>{{ number(item.count) }}</td></tr><tr v-if="!items.length"><td colspan="3">没有符合筛选条件的已识别物品</td></tr></tbody></table></div>
        <p class="domain-caption">共 {{ result.backpack.items?.length || 0 }} 条识别记录<span v-if="items.length > 200">，本次展示前 200 条，请筛选缩小范围</span></p>
        <p v-for="warning in result.backpack.warnings" :key="warning" class="domain-warning">{{ warning }}</p>
      </article>
      <!-- 计划读取现有算法缓存，材料已有/缺口未知时单独标明，不暗示队列执行或完成。 -->
      <article v-if="result.cultivationPlans" class="card domain-card">
        <div class="card-heading"><h3>旧培养配置与需求缓存</h3><span class="subtle-badge">配置与缓存</span></div>
        <p class="domain-caption">当前 {{ result.cultivationPlans.plans?.length || 0 }} 个旧培养配置；与独立保存的培养队列分别展示。材料需求来自服务端缓存，不能证明任务执行或完成。</p>
        <p v-if="supportsQueryRoles" class="domain-caption">计划配置由实例共享；角色等级、天赋与需求缓存按所选查询角色读取。扫描档案没有 UID 绑定，已有数量和缺口不与该角色合并。</p>
        <details v-for="plan in result.cultivationPlans.plans" :key="plan.id" class="cultivation-plan"><summary>{{ plan.oneLong || plan.rankedGroup || '培养计划' }}<span>{{ plan.characters?.length || 0 }} 名角色</span></summary>
          <div v-for="character in plan.characters" :key="character.name" class="cultivation-character"><h4>{{ character.name }}</h4><p>等级 {{ character.level ?? '未知' }} → {{ character.targetLevel ?? '未提供' }} · A/E/Q {{ character.talents?.join('/') || '未知' }} → {{ character.targetTalents?.join('/') || '未提供' }}</p><p class="domain-caption">面板：{{ time(character.panelUpdatedAt) }}<br>{{ character.targetSource === 'calculator-defaults' ? '所示目标为现有计算器默认值，并非计划保存的逐项培养目标。' : '培养目标按服务端已核实来源显示。' }}</p>
            <div v-if="character.requirementsAvailable" class="domain-table-scroll"><table class="domain-table"><thead><tr><th>材料</th><th>需求</th><th>已识别</th><th>还缺</th></tr></thead><tbody><tr v-for="material in character.materials" :key="material.id"><td>{{ material.name }}</td><td>{{ number(material.required) }}</td><td>{{ number(material.owned) }}</td><td>{{ number(material.missing) }}</td></tr></tbody></table></div><p v-else class="domain-caption">该目标的需求缓存暂不可用，请在 AutoBGI 查看或更新培养计划。</p><p v-if="character.warning" class="domain-warning">{{ character.warning }}</p>
          </div>
        </details>
        <p v-for="warning in result.cultivationPlans.warnings" :key="warning" class="domain-warning">{{ warning }}</p>
      </article>
    </template>
    <p v-else-if="supported && !loading" class="domain-caption">点击读取已有收益、扫描背包和培养缓存；此操作不会触发扫描或远程更新。</p>
  </section>
</template>
