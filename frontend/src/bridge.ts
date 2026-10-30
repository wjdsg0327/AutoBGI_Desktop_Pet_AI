// 业务 DTO 从当前宿主的 Wails 生成结果读取，不依赖旧版服务绑定。
import type { AppState } from '../bindings/autobgi-pet/internal/service/models'
import type { ProfileInput, ProfileView } from '../bindings/autobgi-pet/internal/profile/models'
import type { PetDragStart } from '../bindings/autobgi-pet/internal/desktop/models'
import { isNative } from './nativeRuntime'

export type { AppState, ProfileInput, ProfileView, PetDragStart }
export { isNative }

// 原生展示快照不包含凭据，快捷键字段先以未知对象接收，再由状态仓库检查固定白名单。
export interface NativePresentation {
  visible: boolean
  panelVisible?: boolean
  action?: string
  revision: number
  warning?: string
  recoveryShortcut?: unknown
  panelSection?: unknown
  panelTarget?: unknown
  panelSectionRevision?: unknown
}
export interface RecoveryShortcutPresentation {
  requested: string
  registered: string
  status: 'unknown' | 'pending' | 'registered' | 'conflict' | 'unavailable'
  retryable: boolean
  warning: string
}

// 关键词规则只包含本地可编辑字段，不携带网络凭据。
export interface KeywordRuleInput {
  id: string
  keywords: string[]
  action: string
  text: string
  enabled: boolean
  todo?: { enabled: boolean; action: string; target: string }
}

// 所有实际调用都经过宿主显式注册的两个精简服务。
async function invoke<T>(target: 'PetAPI' | 'DesktopAPI', method: string, ...args: unknown[]): Promise<T> {
  if (!isNative()) throw new Error('当前为浏览器界面预览，请启动 Windows 桌宠后连接 AutoBGI。')
  const { Call } = await import('@wailsio/runtime')
  return Call.ByName(`autobgi-pet/internal/bindings.${target}.${method}`, ...args) as Promise<T>
}

// 前端仅调用连接、日志状态和关键词规则。
export const api = {
  getState: () => invoke<AppState>('PetAPI', 'GetState'),
  saveProfile: (input: ProfileInput) => invoke<AppState>('PetAPI', 'SaveProfile', input),
  deleteProfile: (id: string) => invoke<AppState>('PetAPI', 'DeleteProfile', id),
  connect: (id: string) => invoke<AppState>('PetAPI', 'Connect', id),
  disconnect: () => invoke<AppState>('PetAPI', 'Disconnect'),
  updateKeywordRules: (rules: KeywordRuleInput[]) => invoke<AppState>('PetAPI', 'UpdateKeywordRules', rules),
  // 代办动作和目标只从宿主读取，处理时不接受页面传入的任意动作。
  listTodoTargets: (action: string) => invoke<string[]>('PetAPI', 'ListTodoTargets', action),
  processTodo: (id: string) => invoke<AppState>('PetAPI', 'ProcessTodo', id),
  dismissTodo: (id: string) => invoke<AppState>('PetAPI', 'DismissTodo', id),
  // 拾取通知单独保存，避免覆盖尚未保存的关键词草稿。
  updatePickupEnabled: (enabled: boolean) => invoke<AppState>('PetAPI', 'UpdatePickupEnabled', enabled),
}

// 窗口绑定只覆盖当前桌宠的展示、互动与拖动操作。
export async function desktop() {
  if (!isNative()) throw new Error('此操作需要 Windows 桌宠宿主。')
  return {
    GetPresentation: () => invoke<Record<string, unknown>>('DesktopAPI', 'GetPresentation'),
    // 悬停和窗口模式由原生宿主管理，页面只上报当前指针位置。
    SetTodoPetHover: (hover: boolean) => invoke<void>('DesktopAPI', 'SetTodoPetHover', hover),
    SetTodoWindowHover: (hover: boolean) => invoke<void>('DesktopAPI', 'SetTodoWindowHover', hover),
    OpenTodoWindow: () => invoke<void>('DesktopAPI', 'OpenTodoWindow'),
    CloseTodoWindow: () => invoke<void>('DesktopAPI', 'CloseTodoWindow'),
    GetTodoWindowState: () => invoke<{ visible: boolean; expanded: boolean; revision: number }>('DesktopAPI', 'GetTodoWindowState'),
    ShowPet: () => invoke<void>('DesktopAPI', 'ShowPet'),
    ShowPanel: () => invoke<void>('DesktopAPI', 'ShowPanel'),
    ShowContextMenu: () => invoke<void>('DesktopAPI', 'ShowContextMenu'),
    // 分享文件只传递规则草稿，由宿主负责文件选择和格式校验。
    ImportKeywordRulesFile: () => invoke<KeywordRuleInput[] | null>('DesktopAPI', 'ImportKeywordRulesFile'),
    ExportKeywordRulesFile: (rules: KeywordRuleInput[]) => invoke<boolean>('DesktopAPI', 'ExportKeywordRulesFile', rules),
    // 桌宠确认当前动作首帧已经绘制后，宿主才启动该条动作的队列计时。
    StartKeywordTrigger: (id: number, generation: number, attempt: number) => invoke<boolean>('DesktopAPI', 'StartKeywordTrigger', id, generation, attempt),
    // 文档不可见时告知宿主暂停拾取展示，恢复后再逐条播放。
    SetPetDocumentVisible: (visible: boolean) => invoke<void>('DesktopAPI', 'SetPetDocumentVisible', visible),
    PetClicked: () => invoke<void>('DesktopAPI', 'PetClicked'),
    BeginPetDrag: () => invoke<PetDragStart | null>('DesktopAPI', 'BeginPetDrag'),
    DragPetTo: (token: string, x: number, y: number) => invoke<boolean>('DesktopAPI', 'DragPetTo', token, x, y),
    PetDragged: (token: string) => invoke<boolean>('DesktopAPI', 'PetDragged', token),
    CancelPetDrag: (token: string) => invoke<boolean>('DesktopAPI', 'CancelPetDrag', token),
  }
}

export async function listen<T>(name: string, handler: (data: T) => void): Promise<() => void> {
  if (!isNative()) return () => {}
  const { Events } = await import('@wailsio/runtime')
  return Events.On(name, event => handler(event.data as T))
}
