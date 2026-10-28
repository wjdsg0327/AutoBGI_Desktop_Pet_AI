<script setup lang="ts">
import { computed, ref } from 'vue'
import { profileTransfer } from '../bridge'
import { useAppStore } from '../stores/app'
import AppIcon from './AppIcon.vue'

// 使用共享操作锁与服务端状态序号，导入后其他窗口也会收到同一份真实连接列表。
const store = useAppStore()
const message = ref('')
const pending = ref<'import' | 'export' | ''>('')
const count = computed(() => store.state?.profiles?.length || 0)
const disabled = computed(() => !store.native || store.busy || pending.value !== '')

// 原生保存成功才报告导出完成；取消对话框既不提示成功，也不修改连接资料。
async function exportProfiles() {
  if (disabled.value || count.value === 0) return
  message.value = ''
  pending.value = 'export'
  try {
    await store.run(async () => {
      if (await profileTransfer.saveFile()) message.value = '连接资料 JSON 已保存。文件不包含密钥，迁移后需要重新填写。'
    })
  } finally {
    pending.value = ''
  }
}

// 文件整批校验和原子保存均由 Go 完成，界面只接收已经脱敏的新增结果。
async function importProfiles() {
  if (disabled.value || count.value >= 20) return
  message.value = ''
  pending.value = 'import'
  try {
    await store.run(async () => {
      const result = await profileTransfer.importFile()
      if (!result) return
      store.accept(result.state)
      message.value = `已导入 ${result.count} 个连接。请点击对应连接的“编辑”重新填写密钥，并选择系统保存或仅本次会话使用。`
    })
  } finally {
    pending.value = ''
  }
}
</script>

<template>
  <!-- 资料迁移保持为连接页内的次级操作，危险授权不会随导入文件恢复。 -->
  <section class="profile-transfer" aria-label="连接资料导入与导出">
    <div class="transfer-description">
      <span class="transfer-symbol"><AppIcon name="link" :size="18" /></span>
      <div><strong>迁移连接资料</strong><p>JSON 仅保存名称、地址与适配器设置。导入后均为仅查询模式，需要重新填写密钥；当前连接不受影响。</p></div>
    </div>
    <div class="transfer-actions">
      <button type="button" class="transfer-button" :disabled="disabled || count >= 20" :title="count >= 20 ? '已达到 20 个连接上限' : '选择不超过 64 KiB 的连接资料 JSON'" @click="importProfiles">
        <AppIcon name="plus" :size="15" />{{ pending === 'import' ? '等待选择文件…' : '导入资料' }}
      </button>
      <button type="button" class="transfer-button" :disabled="disabled || count === 0" title="打开系统保存对话框，导出不含密钥的 JSON 文件" @click="exportProfiles">
        <AppIcon name="external" :size="15" />{{ pending === 'export' ? '等待保存文件…' : '导出资料' }}
      </button>
    </div>
    <p v-if="message" class="transfer-message" role="status"><AppIcon name="check" :size="15" />{{ message }}</p>
    <p v-if="!store.native" class="transfer-hint">文件选择和保存需要启动 Windows 桌宠宿主。</p>
  </section>
</template>

<style scoped>
/* 使用连接页既有暖色卡片与绿色强调，窄窗口下将文件操作移到说明下方。 */
.profile-transfer{display:flex;align-items:center;flex-wrap:wrap;gap:16px;padding:20px 22px;margin-top:18px;border:1px solid var(--line,#e9e5df);border-radius:16px;background:var(--surface,#fffdf9)}
.transfer-description{display:flex;align-items:flex-start;gap:12px;flex:1;min-width:220px}.transfer-symbol{display:grid;place-items:center;width:34px;height:34px;flex-shrink:0;background:#eff3e9;border-radius:11px;color:#637448}
.transfer-description strong{font-size:13px;color:#3f4537}.transfer-description p{margin:5px 0 0;font-size:11px;line-height:1.8;color:#898a80;max-width:460px}
.transfer-actions{display:flex;gap:8px;flex-shrink:0}.transfer-button{display:flex;align-items:center;justify-content:center;gap:6px;padding:9px 12px;border:1px solid #dedfd5;border-radius:9px;background:#f8f9f3;color:#61734b;font-size:11px;cursor:pointer;white-space:nowrap}.transfer-button:hover:not(:disabled){background:#edf2e4;border-color:#b5c59e}.transfer-button:focus-visible{outline:2px solid #83976b;outline-offset:3px}.transfer-button:disabled{opacity:.45;cursor:not-allowed}
.transfer-message,.transfer-hint{flex-basis:100%;margin:0;font-size:11px;line-height:1.8}.transfer-message{display:flex;align-items:flex-start;gap:6px;color:#577843}.transfer-message svg{flex-shrink:0;margin-top:2px}.transfer-hint{color:#9b8c74}
@media(max-width:680px){.profile-transfer{padding:16px}.transfer-actions{margin-left:46px;flex-wrap:wrap}.transfer-description{min-width:100%}}
</style>
