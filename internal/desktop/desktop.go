package desktop

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/notification"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/service"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// DesktopService 的绑定只操作本程序窗口和当前连接网页，没有通用进程或 URL 能力。
type DesktopService struct {
	app                  *application.App
	service              *service.Service
	pet                  *application.WebviewWindow
	panel                *application.WebviewWindow
	bubble               *application.WebviewWindow
	bubbleCreating       bool
	tray                 *application.SystemTray
	mu                   sync.Mutex
	prefs                profile.Preferences
	notices              bubbleState
	manualNoticeSequence uint64
	petClickSequence     uint64
	keywordActions       map[string]service.LogTrigger
	activeKeywordTrigger *service.LogTrigger
	keywordTriggerUntil  time.Time
	logActionSequence    uint64
	logDispatchSequence  uint64
	activeDispatchID     uint64
	fainted              bool
	faintTimer           *time.Timer
	faintTimerVersion    uint64
	faintActivity        string
	faintGeneration      uint64
	faintStateRevision   uint64
	bubbleText           string
	bubbleKind           string
	manualWarning        bool
	bubbleTimer          *time.Timer
	bubbleTimerVersion   uint64
	bubbleVersion        uint64
	done                 chan struct{}
	once                 sync.Once
	monitorSignature     string
	monitor              monitorTracker
	drag                 petDragState
	dragPreferenceApply  int
	applyRunning         bool
	applyingRevision     uint64
	pendingApply         *service.AppState
	nativeApplyUncertain bool
	positionRestore      *savedPosition
	positionResetPending bool
	shortcut             recoveryShortcutController
	shortcutRetryQueued  bool
	trayWarning          string
	networkWarning       string
	networkWorker        sync.WaitGroup
	panelLifecycle       panelLifecycle
	panelNavigation      panelNavigation
	panelVisible         bool
	documentHidden       bool
	restorePetPending    bool
	appliedRevision      uint64
	presentationRevision uint64
	sleeping             bool
	powerSleepingIntent  bool
	power                powerController
}

// New 由宿主注入唯一业务服务；所有窗口共用它的采集状态。
func New(s *service.Service) *DesktopService {
	return &DesktopService{service: s, done: make(chan struct{})}
}

// Attach 只预建紧凑透明角色与托盘，控制台留到首次设置、恢复故障或用户主动打开。
//
//wails:ignore
func (d *DesktopService) Attach(app *application.App, icon []byte) {
	d.app = app
	// 在建窗和任何事件注册前只读一次历史位置，早到移动事件不能先覆盖尚未读取的恢复依据。
	position, hasPosition := loadPosition()
	d.mu.Lock()
	d.positionResetPending = !hasPosition
	if hasPosition {
		d.positionRestore = &position
	}
	d.mu.Unlock()
	state := d.service.GetState()
	d.prefs = state.Preferences
	d.notices.update(state, false, time.Now())
	p := state.Preferences.Pet
	d.pet = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "pet", Title: "芙宁娜 · 桌宠", URL: "/?view=pet", Width: int(224 * p.Scale), Height: int(240 * p.Scale),
		Frameless: true, DisableResize: true, BackgroundType: application.BackgroundTypeTransparent,
		AlwaysOnTop: p.AlwaysOnTop, Hidden: !p.Visible, DefaultContextMenuDisabled: true,
		Permissions: deniedPermissions(),
		Windows:     application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, WindowDidMoveDebounceMS: 350, ExStyle: petExStyle(p.AlwaysOnTop), Permissions: deniedWindowsPermissions()},
	})
	d.pet.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); d.HidePet() })
	d.pet.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		// Wails 事件由协程投递，完整读写必须与布局纠正共用 UI 队列。
		application.InvokeAsync(func() {
			if !d.stopped() {
				d.rememberPosition()
				d.moveBubble()
			}
		})
	})
	d.pet.OnWindowEvent(events.Common.WindowDPIChanged, func(*application.WindowEvent) {
		// DPI 事件可能早于屏幕枚举刷新，先撤销手势，旧缩放下的绝对目标不得继续应用。
		d.invalidatePetDrag()
		application.InvokeAsync(func() {
			if !d.stopped() {
				d.correctPosition()
			}
		})
	})
	d.pet.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		application.InvokeAsync(func() {
			if d.stopped() {
				return
			}
			d.restorePosition()
			// RuntimeReady 可能晚于恢复交互操作，不能重新使用启动时捕获的穿透偏好。
			d.mu.Lock()
			clickThrough := d.prefs.Pet.ClickThrough
			d.mu.Unlock()
			d.pet.SetIgnoreMouseEvents(clickThrough)
			d.emitPresentation()
		})
	})
	d.service.SetPanelVisible(false)
	// 原生菜单既用于托盘，也用于角色右键；固定目的地和穿透开关随调用读取当前状态。
	menu := application.NewMenu()
	menu.Add("显示桌宠 / 恢复交互").OnClick(func(*application.Context) { d.ShowPet(); d.SetClickThrough(false) })
	menu.Add("桌宠设置").OnClick(func(*application.Context) { d.ShowPanel() })
	menu.Add("切换鼠标穿透").OnClick(func(*application.Context) { d.ToggleClickThrough() })
	menu.Add("关闭鼠标穿透").OnClick(func(*application.Context) { d.SetClickThrough(false) })
	menu.Add("重置桌宠位置").OnClick(func(*application.Context) { d.ResetPosition() })
	menu.Add("隐藏桌宠（保持连接）").OnClick(func(*application.Context) { d.HidePet() })
	menu.AddSeparator()
	menu.Add("断开连接").OnClick(func(*application.Context) { d.service.Disconnect() })
	menu.AddSeparator()
	menu.Add("退出桌宠").OnClick(func(*application.Context) { d.Quit() })
	d.tray = app.SystemTray.New()
	d.tray.SetIcon(icon)
	d.tray.SetTooltip("芙宁娜桌宠 · 双击打开设置")
	d.tray.SetMenu(menu)
	d.tray.OnClick(d.ShowPet)
	d.tray.OnDoubleClick(d.ShowPanel)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		d.startDesktop(needsInitialSetup(state.Profiles, state.ActiveProfileID), p.RecoveryShortcut)
	})
	d.bindPowerEvents(app)
	d.bindNetworkEvents(app)
}

// Apply 只消费脱敏快照，按变化更新窗口，不因每次轮询反复显示或夺取焦点。
//
//wails:ignore
func (d *DesktopService) Apply(state service.AppState) {
	if d.stopped() {
		return
	}
	application.InvokeAsync(func() {
		if d.stopped() {
			return
		}
		// 重入只保留最高版本，不能在外层原生调用尚未返回时再次写入窗口。
		d.mu.Lock()
		if state.Revision > max(d.appliedRevision, d.applyingRevision) &&
			(d.pendingApply == nil || state.Revision > d.pendingApply.Revision) {
			pending := state
			d.pendingApply = &pending
			if !samePetDragPreferences(d.prefs, state.Preferences) {
				// 中间偏好即使随后被更新合并，也必须先撤销现有手势并保持整个消费周期的门禁。
				d.drag.invalidate()
				d.dragPreferenceApply = 1
			}
		}
		if d.applyRunning || d.pendingApply == nil {
			d.mu.Unlock()
			return
		}
		d.applyRunning = true
		d.mu.Unlock()
		d.drainDesktopApply()
	})
}

// drainDesktopApply 等当前原生调用完整返回才处理重入更新，最终窗口事实由最高版本覆盖。
func (d *DesktopService) drainDesktopApply() {
	completed := false
	var inFlight *service.AppState
	defer func() {
		d.mu.Lock()
		d.applyRunning = false
		d.applyingRevision = 0
		d.dragPreferenceApply = 0
		if !completed {
			// 异常后保留最高待处理意图并失败关闭，后续快照可以重新完整应用，不永久锁死消费者。
			d.nativeApplyUncertain = true
			d.drag.invalidate()
			if inFlight != nil && (d.pendingApply == nil || inFlight.Revision > d.pendingApply.Revision) {
				d.pendingApply = inFlight
			}
		}
		d.mu.Unlock()
	}()
	for {
		d.mu.Lock()
		if d.stopped() || d.pendingApply == nil {
			completed = true
			d.mu.Unlock()
			return
		}
		state := *d.pendingApply
		d.pendingApply = nil
		d.applyingRevision = state.Revision
		before := d.prefs
		force := d.nativeApplyUncertain
		inFlight = &state
		if force || !samePetDragPreferences(before, state.Preferences) {
			d.dragPreferenceApply = 1
			d.drag.invalidate()
		}
		d.mu.Unlock()
		// 相关门禁持续覆盖整个消费周期；无关轮询和同值偏好不会取消正常手势。
		p := state.Preferences.Pet
		if force || p.Scale != before.Pet.Scale {
			d.pet.SetSize(int(224*p.Scale), int(240*p.Scale))
			d.correctPosition()
		}
		if force || p.AlwaysOnTop != before.Pet.AlwaysOnTop {
			d.pet.SetAlwaysOnTop(p.AlwaysOnTop)
		}
		if force || p.ClickThrough != before.Pet.ClickThrough {
			d.pet.SetIgnoreMouseEvents(p.ClickThrough)
		}
		if force || p.RecoveryShortcut != before.Pet.RecoveryShortcut {
			d.updateShortcut(p.RecoveryShortcut)
		}
		if force || p.Visible != before.Pet.Visible {
			if p.Visible {
				d.pet.Show()
			} else {
				d.pet.Hide()
			}
		}
		// 只有本轮全部原生操作完成才提交实际偏好，重入更新仍在下一轮统一消费。
		d.mu.Lock()
		d.prefs = state.Preferences
		d.appliedRevision = state.Revision
		d.nativeApplyUncertain = false
		// 拾取开关关闭时立即撤销尚未完成的所有蛋糕动作。
		if !state.Preferences.Notifications.PickupEnabled && d.notices.center != nil {
			d.notices.center.ClearPickups()
			for id := range d.keywordActions {
				if strings.HasPrefix(id, "pickup:") {
					delete(d.keywordActions, id)
				}
			}
		}
		d.syncFaintLocked(state, time.Now())
		// 连接切换时不再向新建窗口补发旧连接的动作。
		if d.activeKeywordTrigger != nil && d.activeKeywordTrigger.Generation != state.Connection.Generation {
			d.activeKeywordTrigger = nil
		}
		if d.notices.scope != notificationScope(state) {
			// 切换资料时旧队列已失效，清除对应动作索引。
			d.keywordActions = nil
		}
		plan := d.notices.update(d.visibleNotificationStateLocked(state), d.sleeping, time.Now())
		d.prepareBubbleLocked(plan)
		d.mu.Unlock()
		d.app.Event.Emit("pet:state", state)
		d.emitPresentation()
		d.dispatchBubble(plan)
		inFlight = nil
	}
}

// GetPresentation 在 WebView 完成订阅后读取当前原生展示状态，避免错过首帧事件。
func (d *DesktopService) GetPresentation() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.presentationLocked()
}

// presentationLocked 让初始化读取与广播共用同一份显隐合同，面板显隐独立于角色偏好。
func (d *DesktopService) presentationLocked() map[string]any {
	action := ""
	if d.sleeping {
		action = "sleep"
	} else if d.fainted {
		action = "fainted"
	} else if d.manualWarning {
		action = "warning"
	}
	shortcut := d.shortcut.snapshot(d.prefs.Pet.RecoveryShortcut, d.shortcutRetryQueued)
	var keywordTrigger any
	if d.keywordTriggerVisibleLocked(time.Now()) {
		keywordTrigger = d.triggerPayloadLocked(*d.activeKeywordTrigger)
	}
	return map[string]any{"visible": d.prefs.Pet.Visible && !d.sleeping && !d.documentHidden, "panelVisible": d.panelVisible, "action": action, "revision": d.presentationRevision, "text": d.bubbleText, "kind": d.bubbleKind, "warning": d.hostWarningLocked(shortcut.Warning), "recoveryShortcut": shortcut, "keywordTrigger": keywordTrigger,
		"panelSection": d.panelNavigation.section, "panelTarget": d.panelNavigation.target, "panelSectionRevision": d.panelNavigation.revision}
}

// keywordTriggerVisibleLocked 只补取仍在当前队列和有效连接中的动作，确认等待超时后不再暴露。
func (d *DesktopService) keywordTriggerVisibleLocked(now time.Time) bool {
	trigger := d.activeKeywordTrigger
	if trigger == nil || d.stopped() || d.sleeping || d.powerSleepingIntent || d.documentHidden ||
		!d.prefs.Pet.Visible || !d.prefs.Notifications.Bubble || d.prefs.Notifications.QuietHoursEnabled ||
		d.notices.scope.Generation != trigger.Generation || d.notices.entryID != logActionID(*trigger) ||
		d.notices.center == nil || d.service == nil {
		return false
	}
	state := d.service.GetState()
	if state.Connection.Generation != trigger.Generation || trigger.Action == "eat" && !state.Preferences.Notifications.PickupEnabled {
		return false
	}
	deadline, timed := d.notices.center.NextDeadline()
	return timed && now.Before(deadline) && (d.keywordTriggerUntil.IsZero() || now.Before(d.keywordTriggerUntil))
}

// triggerPayloadLocked 同时服务实时广播和晚就绪窗口的当前动作补取。
func (d *DesktopService) triggerPayloadLocked(trigger service.LogTrigger) map[string]any {
	expiresAt := ""
	confirmUntil := ""
	pending := d.keywordTriggerUntil.IsZero()
	if pending && d.notices.center != nil {
		if deadline, timed := d.notices.center.NextDeadline(); timed {
			confirmUntil = deadline.Format(time.RFC3339Nano)
		}
	} else if !pending {
		expiresAt = d.keywordTriggerUntil.Format(time.RFC3339Nano)
	}
	return map[string]any{"id": trigger.ID, "generation": trigger.Generation, "action": trigger.Action, "attempt": d.activeDispatchID,
		"text": trigger.Text, "keyword": trigger.Keyword, "pending": pending, "expiresAt": expiresAt, "confirmUntil": confirmUntil}
}

// emitPresentation 发布有序的原生展示状态，隐藏和睡眠时前端据此停止不必要的计时与渲染。
func (d *DesktopService) emitPresentation() {
	d.mu.Lock()
	d.presentationRevision++
	snapshot := d.presentationLocked()
	d.mu.Unlock()
	d.app.Event.Emit("pet:presentation", snapshot)
}

// updateShortcut 先注册新的恢复组合再释放旧组合；冲突时保留可用的旧恢复入口。
func (d *DesktopService) updateShortcut(shortcut string) {
	d.shortcut.apply(shortcut, func(value string) error {
		return d.app.GlobalShortcut.Register(value, d.RestoreFromHost)
	}, d.app.GlobalShortcut.Unregister)
	d.emitPresentation()
}

// RetryRecoveryShortcut 只重试当前已保存的白名单组合，不接受来自 WebView 的按键参数。
func (d *DesktopService) RetryRecoveryShortcut() error {
	if d.stopped() || d.app == nil || d.service == nil {
		return errors.New("桌面宿主尚未就绪，请从系统托盘恢复交互")
	}
	if !supportedRecoveryShortcut(d.service.GetState().Preferences.Pet.RecoveryShortcut) {
		return errors.New("请先在设置中选择支持的恢复快捷键")
	}
	d.mu.Lock()
	if d.shortcutRetryQueued {
		d.mu.Unlock()
		return nil
	}
	d.shortcutRetryQueued = true
	d.mu.Unlock()
	d.emitPresentation()
	application.InvokeAsync(func() {
		// 只排一个 UI 线程重试，执行时重新读取偏好，不能注册已被用户更换的旧请求。
		defer func() {
			d.mu.Lock()
			d.shortcutRetryQueued = false
			d.mu.Unlock()
			if !d.stopped() {
				d.emitPresentation()
			}
		}()
		if !d.stopped() {
			d.updateShortcut(d.service.GetState().Preferences.Pet.RecoveryShortcut)
		}
	})
	return nil
}

// ShowPanel 仅用户显式调用时聚焦长面板，普通业务提醒不会调用此入口。
func (d *DesktopService) ShowPanel() {
	d.requestPanel(true, true)
}

// HidePanel 关闭只隐藏已有面板，不创建窗口，也不清空面板编辑内容或业务连接。
func (d *DesktopService) HidePanel() { d.requestPanel(false, false) }

// setPetPreference 使用业务服务的原子配置写入，不维护另一套设置文件。
func (d *DesktopService) setPetPreference(change func(*profile.PetPreferences)) bool {
	if d.stopped() {
		return false
	}
	state := d.service.GetState()
	change(&state.Preferences.Pet)
	if _, err := d.service.UpdatePreferences(state.Preferences); err != nil {
		d.showBubble("桌宠设置保存失败，请打开控制台检查", "warning")
		return false
	}
	return true
}

// ShowPet 通过无激活工具窗恢复角色，不调用 Focus，因此不会主动切走游戏输入。
func (d *DesktopService) ShowPet() {
	if !d.setPetPreference(func(p *profile.PetPreferences) { p.Visible = true }) {
		return
	}
	d.pet.Show()
	d.correctPosition()
	d.emitPresentation()
}

// HidePet 只隐藏动画窗口，采集仍继续运行。
func (d *DesktopService) HidePet() {
	if !d.setPetPreference(func(p *profile.PetPreferences) { p.Visible = false }) {
		return
	}
	// 偏好广播可能仍在 UI 队列中，立即阻止气泡并丢弃待显示项。
	d.mu.Lock()
	d.prefs.Pet.Visible = false
	d.drag.invalidate()
	plan := d.notices.update(d.notificationStateLocked(), d.sleeping, time.Now())
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()
	d.pet.Hide()
	d.dispatchBubble(plan)
}

// SetClickThrough 使用原生 IgnoreMouseEvents，恢复入口始终在托盘和快捷键中。
func (d *DesktopService) SetClickThrough(enabled bool) {
	if !d.setPetPreference(func(p *profile.PetPreferences) { p.ClickThrough = enabled }) {
		return
	}
	d.pet.SetIgnoreMouseEvents(enabled)
}

// ToggleClickThrough 在调用时读取已保存偏好，避免复用启动时的旧菜单值。
func (d *DesktopService) ToggleClickThrough() {
	d.SetClickThrough(!d.service.GetState().Preferences.Pet.ClickThrough)
}

// SetAlwaysOnTop 只影响桌宠窗口，不提升进程权限。
func (d *DesktopService) SetAlwaysOnTop(enabled bool) {
	if !d.setPetPreference(func(p *profile.PetPreferences) { p.AlwaysOnTop = enabled }) {
		return
	}
	d.pet.SetAlwaysOnTop(enabled)
}

// SetScale 的范围再次由后端偏好校验，不允许前端创建无限大的透明窗口。
func (d *DesktopService) SetScale(scale float64) {
	if scale >= 0.6 && scale <= 2 {
		d.setPetPreference(func(p *profile.PetPreferences) { p.Scale = scale })
	}
}

// OpenAutoBGI 只打开当前已校验的 profile 基础地址，不携带 apiKey。
func (d *DesktopService) OpenAutoBGI() error {
	url, err := d.service.ActiveURL()
	if err != nil {
		return errors.New("请先保存并选择一个连接")
	}
	if d.app.Browser.OpenURL(url) != nil {
		return errors.New("无法打开系统浏览器")
	}
	return nil
}

// ShowContextMenu 复用原生托盘菜单，不能接受前端传入任意菜单命令。
func (d *DesktopService) ShowContextMenu() { d.tray.ShowMenu() }

// PetClicked 按需展示当前 BGI 状态，较早请求不能覆盖较新的点击结果。
func (d *DesktopService) PetClicked() {
	generation := d.service.GetState().Connection.Generation
	d.mu.Lock()
	d.petClickSequence++
	sequence := d.petClickSequence
	d.mu.Unlock()
	// 网络读取在后台进行，避免阻塞桌宠和设置窗口。
	go func() {
		text, err := d.service.QueryBGIStatus()
		kind := "status"
		if err != nil {
			text = err.Error()
			kind = "info"
		}
		// 检查序号和排队必须同时持锁，避免新的点击插入旧结果的提交过程。
		now := time.Now()
		d.mu.Lock()
		// 在桌面锁内重新读取连接并核对队列来源，不能把旧结果归入新的通知范围。
		state := d.service.GetState()
		if d.stopped() || !petStatusCurrent(sequence, generation, d.petClickSequence, state, d.notices.scope) {
			d.mu.Unlock()
			return
		}
		d.manualNoticeSequence++
		plan := d.notices.offer(notification.Entry{ID: fmt.Sprintf("manual:%d", d.manualNoticeSequence), Kind: kind, Message: text, CreatedAt: now.Format(time.RFC3339Nano)}, now)
		d.prepareBubbleLocked(plan)
		d.mu.Unlock()
		d.dispatchBubble(plan)
	}()
}

// petStatusCurrent 同时核验点击顺序、实时连接与通知队列的来源。
func petStatusCurrent(sequence, generation, latestSequence uint64, state service.AppState, scope notification.Scope) bool {
	return sequence == latestSequence && state.Connection.Generation == generation && notificationScope(state) == scope
}

// BeginPetDrag 返回一次手势令牌和逻辑原点，后续位移必须携带同一仍有效的令牌。
// 不使用系统 modal 移动循环：那种拖拽会独占全局鼠标捕获，窗口繁忙时整个桌面会像卡死。
func (d *DesktopService) BeginPetDrag() *PetDragStart {
	if d.stopped() {
		return nil
	}
	return application.InvokeSyncWithResult(func() *PetDragStart {
		// 新的开始请求先撤销旧手势；枚举、原点或随机源失败均保持失败关闭。
		beginRevision := d.invalidatePetDrag()
		// 与显示、隐藏和电源消息按同一 UI 顺序消费，不能在排队前确认后继续使用旧门禁。
		if !d.petDragAllowed() {
			return nil
		}
		screens := d.app.Screen.GetAll()
		w, h := d.pet.Size()
		if w < 1 || h < 1 || len(monitorWorkAreas(screens)) == 0 {
			return nil
		}
		d.mu.Lock()
		restoring := d.positionRestore != nil || d.positionResetPending
		d.mu.Unlock()
		// 启动恢复可暂缺目标屏工作区；开始手势保留快照，实际有效移动才替换恢复意图。
		if restoring {
			d.restorePendingPosition(screens)
		} else if !d.correctPendingPosition(screens) {
			return nil
		}
		x, y := d.pet.Position()
		// 恢复窗口可能触发嵌套消息，取恢复后的真实尺寸，并在登记前再次检查门禁。
		environment := d.petDragEnvironmentOnScreens(screens)
		token, err := newPetDragToken()
		if err != nil {
			return nil
		}
		d.mu.Lock()
		accepted := d.petDragAllowedLocked() && d.drag.revision == beginRevision && d.drag.begin(token, environment)
		if accepted {
			// 首次监视 tick 不应把相同布局当成新变化，在手势中途额外夹取窗口。
			d.monitorSignature = environment.layout
		}
		d.mu.Unlock()
		if !accepted {
			return nil
		}
		return &PetDragStart{Token: token, X: x, Y: y}
	})
}

// DragPetTo 先验证当前手势和布局，再移动窗口；旧目标不能覆盖新布局已完成的校正。
func (d *DesktopService) DragPetTo(token string, x, y int) bool {
	if d.stopped() {
		return false
	}
	return application.InvokeSyncWithResult(func() bool {
		if !d.petDragAllowed() {
			d.cancelPetDrag(token)
			return false
		}
		screens := d.app.Screen.GetAll()
		environment := d.petDragEnvironmentOnScreens(screens)
		d.mu.Lock()
		accepted := d.petDragAllowedLocked() && d.drag.accepts(token, environment)
		restoring := d.positionRestore != nil || d.positionResetPending
		d.mu.Unlock()
		if !accepted {
			return false
		}
		// 拖动时保留当前台词，并在移动后让气泡跟随桌宠。
		screens = d.app.Screen.GetAll()
		environment = d.petDragEnvironmentOnScreens(screens)
		w, h := environment.width, environment.height
		cx, cy, ok := clampToDesktop(x, y, w, h, monitorWorkAreas(screens))
		d.mu.Lock()
		// 隐藏气泡可能触发嵌套消息，更新锚点前再次核验令牌，不在锁内操作原生窗口。
		accepted = d.petDragAllowedLocked() && d.drag.accepts(token, environment) && ok
		restoring = d.positionRestore != nil || d.positionResetPending
		if accepted {
			if restoring {
				// 有效用户移动仍可取代等待中的启动恢复，保留部分边缘可见的拖动规则。
				accepted = d.monitor.adopt(cx, cy, w, h, screens)
			} else {
				accepted = d.monitor.remember(cx, cy, w, h, screens)
			}
		}
		if accepted {
			d.positionRestore = nil
			d.positionResetPending = false
		} else {
			d.drag.cancel(token)
		}
		d.mu.Unlock()
		if !accepted {
			return false
		}
		// 锚点先于原生移动提交；逐帧仅更新内存，结束或防抖消息负责落盘。
		d.pet.SetPosition(cx, cy)
		d.moveBubbleOnScreens(screens)
		return true
	})
}

// PetDragged 只为仍匹配的手势保存一次，旧结束请求不能消费新手势。
func (d *DesktopService) PetDragged(token string) bool {
	if d.stopped() {
		return false
	}
	return application.InvokeSyncWithResult(func() bool {
		if !d.petDragAllowed() {
			d.cancelPetDrag(token)
			return false
		}
		screens := d.app.Screen.GetAll()
		environment := d.petDragEnvironmentOnScreens(screens)
		x, y := d.pet.Position()
		// 原点读取也可能重入偏好提交，最后一次纯服务读取定义本次结束的校验边界。
		generation, preferences := d.service.GetPetDragPolicy()
		d.mu.Lock()
		accepted := d.petDragAllowedLocked() && generation == environment.generation &&
			samePetDragPreferences(preferences, d.prefs) && d.drag.finish(token, environment)
		if accepted {
			// 结束只保存已经接纳的真实位置，不调用会再次读取原生环境的通用保存入口。
			accepted = d.positionRestore == nil && !d.positionResetPending &&
				d.monitor.remember(x, y, environment.width, environment.height, screens)
		} else {
			d.drag.cancel(token)
		}
		position := d.positionSnapshotLocked(x, y)
		d.mu.Unlock()
		if !accepted {
			return false
		}
		savePosition(position)
		return true
	})
}

// CancelPetDrag 只取消指定手势，不移动窗口、不保存位置，也不取消后来的新令牌。
func (d *DesktopService) CancelPetDrag(token string) bool {
	if d.stopped() {
		return false
	}
	return application.InvokeSyncWithResult(func() bool { return d.cancelPetDrag(token) })
}

// cancelPetDrag 供同一 UI 事务内部使用，取消匹配令牌时不进入任何原生操作。
func (d *DesktopService) cancelPetDrag(token string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.drag.cancel(token)
}

// invalidatePetDrag 记录明确中断；恢复原来的显示、缩放或工作区也不能复活旧手势。
func (d *DesktopService) invalidatePetDrag() uint64 {
	d.mu.Lock()
	d.drag.invalidate()
	revision := d.drag.revision
	d.mu.Unlock()
	return revision
}

// petDragEnvironmentOnScreens 先读取原生尺寸，再复制内存偏好，避免持宿主锁进入窗口调用。
func (d *DesktopService) petDragEnvironmentOnScreens(screens []*application.Screen) petDragEnvironment {
	w, h := d.pet.Size()
	generation, preferences := d.service.GetPetDragPolicy()
	d.mu.Lock()
	scale := d.prefs.Pet.Scale
	matches := samePetDragPreferences(preferences, d.prefs)
	d.mu.Unlock()
	layout := ""
	// 服务偏好可能已经保存而窗口尚未应用；旧尺寸不能被当成新缩放下的合法原点。
	if matches && w == int(224*scale) && h == int(240*scale) && len(monitorWorkAreas(screens)) > 0 {
		layout = monitorLayoutSignature(screens)
	}
	return petDragEnvironment{layout: layout, width: w, height: h, scale: scale, generation: generation}
}

// samePetDragPreferences 只比较会改变拖动解释或交互门禁的字段，普通通知和置顶不打断手势。
func samePetDragPreferences(before, after profile.Preferences) bool {
	return before.Pet.Visible == after.Pet.Visible && before.Pet.ClickThrough == after.Pet.ClickThrough &&
		before.Pet.Scale == after.Pet.Scale &&
		before.Notifications.QuietHoursEnabled == after.Notifications.QuietHoursEnabled
}

// petDragAllowed 同时保护已排队的绑定调用，隐藏、穿透和休息期间不接受迟到的拖动。
func (d *DesktopService) petDragAllowed() bool {
	if d.stopped() {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.petDragAllowedLocked()
}

// petDragAllowedLocked 同时检查已应用状态和尚未消费的睡眠意图，调用方必须持有宿主锁。
func (d *DesktopService) petDragAllowedLocked() bool {
	return !d.stopped() && d.prefs.Pet.Visible && !d.prefs.Pet.ClickThrough &&
		!d.sleeping && !d.powerSleepingIntent && d.dragPreferenceApply == 0 &&
		!d.nativeApplyUncertain && !d.prefs.Notifications.QuietHoursEnabled
}

// stopped 也保护已经排入 UI 队列的回调，退出后不能重新建立窗口或定时器。
func (d *DesktopService) stopped() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

// notificationStateLocked 只为原生显隐事件构造当前策略，不伪造服务端通知或连接身份。
func (d *DesktopService) notificationStateLocked() service.AppState {
	scope := d.notices.scope
	state := service.AppState{ActiveProfileID: scope.ProfileID, Preferences: d.prefs, Connection: service.ConnectionState{ProfileID: scope.ProfileID, Generation: scope.Generation}}
	if scope.InstanceID != "" {
		state.Capabilities = &autobgi.Capabilities{InstanceID: &scope.InstanceID}
	}
	return d.visibleNotificationStateLocked(state)
}

// visibleNotificationStateLocked 仅修改临时通知策略，页面隐藏不写入用户设置。
func (d *DesktopService) visibleNotificationStateLocked(state service.AppState) service.AppState {
	if d.documentHidden {
		state.Preferences.Pet.Visible = false
	}
	return state
}

// SetPetDocumentVisible 将页面可见性同步给宿主，隐藏时暂停动作并在恢复后重新确认。
//
//wails:ignore
func (d *DesktopService) SetPetDocumentVisible(visible bool) {
	state := d.service.GetState()
	d.mu.Lock()
	if d.stopped() || d.documentHidden == !visible {
		d.mu.Unlock()
		return
	}
	d.documentHidden = !visible
	plan := d.notices.update(d.visibleNotificationStateLocked(state), d.sleeping, time.Now())
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()
	d.emitPresentation()
	d.dispatchBubble(plan)
}

// cancelUnstartedLogTrigger 仅跳过指定的未展示动作，过时回调不能隐藏较新的提醒。
func (d *DesktopService) cancelUnstartedLogTrigger(id string) {
	d.mu.Lock()
	plan, canceled := d.notices.cancelUnstartedTrigger(id, time.Now())
	if canceled {
		delete(d.keywordActions, id)
		d.prepareBubbleLocked(plan)
	}
	d.mu.Unlock()
	if canceled {
		d.dispatchBubble(plan)
	}
}

// showBubble 将本地手动摘要也交由中心排队，避免绕开优先级和提醒开关。
func (d *DesktopService) showBubble(text, kind string) {
	now := time.Now()
	d.mu.Lock()
	if d.stopped() {
		d.mu.Unlock()
		return
	}
	// 设置写入失败属于本地系统错误，不能被普通点击摘要降低优先级。
	if kind == "warning" {
		kind = "error"
	}
	d.manualNoticeSequence++
	plan := d.notices.offer(notification.Entry{ID: fmt.Sprintf("manual:%d", d.manualNoticeSequence), Kind: kind, Message: text, CreatedAt: now.Format(time.RFC3339Nano)}, now)
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()
	d.dispatchBubble(plan)
}

// QueueLogTrigger 不阻塞日志读取；连续命中依次展示，动作在对应台词出现时同步播放。
//
//wails:ignore
func (d *DesktopService) QueueLogTrigger(trigger service.LogTrigger) {
	d.queueLogAction(trigger, false)
}

// syncFaintLocked 以服务端收到日志的时间安排饿晕；只有在线订阅参与计时。
func (d *DesktopService) syncFaintLocked(state service.AppState, now time.Time) {
	if state.Revision < d.faintStateRevision {
		return
	}
	d.faintStateRevision = state.Revision
	activity, err := time.Parse(time.RFC3339Nano, state.LogStream.LastActivityAt)
	online := state.LogStream.Status == "online" && err == nil
	fainted := online && !now.Before(activity.Add(5*time.Minute))
	changed := d.fainted != fainted
	if changed && fainted && d.activeKeywordTrigger != nil &&
		(d.activeKeywordTrigger.Action != "eat" || state.Preferences.Notifications.PickupEnabled) {
		// 被打断的动作回到队首，动作资料也必须保留供恢复时重新派发。
		if d.keywordActions == nil {
			d.keywordActions = make(map[string]service.LogTrigger)
		}
		d.keywordActions[logActionID(*d.activeKeywordTrigger)] = *d.activeKeywordTrigger
	}
	d.fainted = fainted
	if d.notices.center == nil {
		// 饿晕计时可能早于窗口通知中心初始化，先同步状态再接收队列。
		d.notices.center = notification.New()
	}
	d.notices.center.SetFainted(fainted)
	if d.faintTimer != nil {
		d.faintTimer.Stop()
		d.faintTimer = nil
	}
	d.faintTimerVersion++
	version := d.faintTimerVersion
	d.faintActivity, d.faintGeneration = state.LogStream.LastActivityAt, state.Connection.Generation
	if !online || fainted || d.stopped() {
		return
	}
	d.faintTimer = time.AfterFunc(max(time.Duration(0), time.Until(activity.Add(5*time.Minute))), func() {
		application.InvokeAsync(func() {
			if d.stopped() {
				return
			}
			latest := d.service.GetState()
			d.mu.Lock()
			if version != d.faintTimerVersion {
				d.mu.Unlock()
				return
			}
			d.syncFaintLocked(latest, time.Now())
			plan := d.notices.advance(time.Now())
			d.prepareBubbleLocked(plan)
			d.mu.Unlock()
			d.emitPresentation()
			d.dispatchBubble(plan)
		})
	})
}

// QueueLogPickup 将拾取物名称转换为独立的吃蛋糕动作。
//
//wails:ignore
func (d *DesktopService) QueueLogPickup(pickup service.LogPickup) {
	d.queueLogAction(service.LogTrigger{Generation: pickup.Generation, Action: "eat", Keyword: pickup.Name}, true)
}

// logActionID 区分动作来源，避免拾取通知和关键词规则互相覆盖。
func logActionID(trigger service.LogTrigger) string {
	if trigger.Action == "eat" {
		return fmt.Sprintf("pickup:%d", trigger.ID)
	}
	return fmt.Sprintf("keyword:%d", trigger.ID)
}

// queueLogAction 在桌面锁内统一编号和排队，保证两类动作按接收顺序播放。
func (d *DesktopService) queueLogAction(trigger service.LogTrigger, pickup bool) {
	now := time.Now()
	state := d.service.GetState()
	// 连接切换后的迟到日志不能落到新的桌宠会话。
	if trigger.Generation != state.Connection.Generation {
		return
	}
	d.mu.Lock()
	// 再读取最新配置，避免开关刚关闭时迟到的拾取重新进入队列。
	state = d.service.GetState()
	if d.stopped() || trigger.Generation != state.Connection.Generation || trigger.Generation < d.notices.scope.Generation ||
		pickup && !state.Preferences.Notifications.PickupEnabled {
		d.mu.Unlock()
		return
	}
	if trigger.Generation > d.notices.scope.Generation {
		// 监听可能快于窗口状态广播，先切换队列来源，避免稍后的快照清空第一条命中。
		d.notices.update(d.visibleNotificationStateLocked(state), d.sleeping, now)
		d.keywordActions = nil
	}
	d.syncFaintLocked(state, now)
	d.logActionSequence++
	trigger.ID = d.logActionSequence
	id := logActionID(trigger)
	entry := notification.Entry{ID: id, Kind: "trigger", Message: trigger.Text, CreatedAt: now.Format(time.RFC3339Nano)}
	var plan bubblePlan
	var accepted, queueFull bool
	if pickup {
		entry.Kind, entry.Message = "pickup", trigger.Keyword
		plan, accepted = d.notices.offerPickup(entry, now)
	} else {
		plan, accepted, queueFull = d.notices.offerTrigger(entry, now)
	}
	if accepted {
		if d.keywordActions == nil {
			d.keywordActions = make(map[string]service.LogTrigger)
		}
		d.keywordActions[id] = trigger
	}
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()
	d.dispatchBubble(plan)
	if queueFull {
		d.service.RecordDroppedTrigger(trigger.Generation)
	}
}

// emitLogTrigger 发布等待首帧确认的动作，已发布动作不会被重复派发或延长等待时间。
func (d *DesktopService) emitLogTrigger(trigger service.LogTrigger) bool {
	d.mu.Lock()
	state := d.service.GetState()
	if d.stopped() || d.sleeping || d.powerSleepingIntent || d.documentHidden || !d.prefs.Pet.Visible ||
		!d.prefs.Notifications.Bubble || d.prefs.Notifications.QuietHoursEnabled ||
		state.Connection.Generation != trigger.Generation || d.notices.scope.Generation != trigger.Generation ||
		trigger.Action == "eat" && !state.Preferences.Notifications.PickupEnabled || d.fainted {
		d.mu.Unlock()
		return false
	}
	id := logActionID(trigger)
	// 重入派发只复用已有动作，不能重复发送或撤销正在等待确认的动作。
	if d.activeKeywordTrigger != nil && d.activeKeywordTrigger.ID == trigger.ID &&
		d.activeKeywordTrigger.Generation == trigger.Generation && d.notices.entryID == id {
		d.mu.Unlock()
		return true
	}
	plan, waiting := d.notices.awaitTrigger(id, time.Now())
	if !waiting {
		d.mu.Unlock()
		return false
	}
	copy := trigger
	d.activeKeywordTrigger = &copy
	d.logDispatchSequence++
	d.activeDispatchID = d.logDispatchSequence
	d.keywordTriggerUntil = time.Time{}
	d.prepareBubbleLocked(plan)
	payload := d.triggerPayloadLocked(trigger)
	d.mu.Unlock()
	d.app.Event.Emit("pet:trigger", payload)
	return true
}

// StartKeywordTrigger 接受当前播放轮次的首帧确认，旧轮次不能启动新的计时。
//
//wails:ignore
func (d *DesktopService) StartKeywordTrigger(id uint64, generation uint64, attempt uint64) bool {
	d.mu.Lock()
	trigger := d.activeKeywordTrigger
	state := d.service.GetState()
	if !d.matchesLogAttemptLocked(id, generation, attempt) ||
		!d.keywordTriggerUntil.IsZero() || d.stopped() || d.sleeping || d.powerSleepingIntent || d.documentHidden ||
		!d.prefs.Pet.Visible || !d.prefs.Notifications.Bubble || d.prefs.Notifications.QuietHoursEnabled ||
		d.notices.scope.Generation != generation || state.Connection.Generation != generation || d.fainted ||
		trigger.Action == "eat" && !state.Preferences.Notifications.PickupEnabled {
		d.mu.Unlock()
		return false
	}
	entryID := logActionID(*trigger)
	// 等待期限内确认后才启动五秒，前端与气泡共用本次动作的绝对截止时间。
	plan, started := d.notices.startTrigger(entryID, time.Now())
	if !started {
		d.mu.Unlock()
		return false
	}
	d.keywordTriggerUntil = plan.deadline
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()
	d.dispatchConfirmedKeywordBubble(plan, generation)
	return true
}

// matchesLogAttemptLocked 同时核对动作身份和每次重新派发的独立轮次。
func (d *DesktopService) matchesLogAttemptLocked(id, generation, attempt uint64) bool {
	trigger := d.activeKeywordTrigger
	return trigger != nil && trigger.ID == id && trigger.Generation == generation && attempt != 0 && attempt == d.activeDispatchID
}

// canShowKeywordBubbleLocked 复核已确认的身份和期限，迟到的原生窗口回调不能显示旧台词。
func (d *DesktopService) canShowKeywordBubbleLocked(plan bubblePlan, generation uint64, now time.Time) bool {
	trigger := d.activeKeywordTrigger
	return trigger != nil && trigger.Generation == generation && plan.entry.Kind == "trigger" &&
		plan.entry.ID == logActionID(*trigger) && d.notices.entryID == plan.entry.ID &&
		d.notices.scope.Generation == generation && d.bubbleVersion == plan.version &&
		!d.keywordTriggerUntil.IsZero() && d.keywordTriggerUntil.Equal(plan.deadline) &&
		now.Before(d.keywordTriggerUntil) && !d.stopped() && !d.sleeping && !d.powerSleepingIntent && !d.documentHidden &&
		d.prefs.Pet.Visible && d.prefs.Notifications.Bubble && !d.prefs.Notifications.QuietHoursEnabled &&
		d.service != nil && d.service.GetState().Connection.Generation == generation
}

// canPrepareKeywordBubbleLocked 确保预置台词仍属于当前动作，原生窗口重入时不能覆盖新台词。
func (d *DesktopService) canPrepareKeywordBubbleLocked(plan bubblePlan, generation uint64) bool {
	trigger, ok := d.logTriggerLocked(plan.entry.ID)
	return ok && trigger.Generation == generation && plan.entry.Kind == "trigger" &&
		d.notices.entryID == plan.entry.ID && d.notices.scope.Generation == generation &&
		d.bubbleVersion == plan.version && !d.stopped() && !d.sleeping && !d.powerSleepingIntent && !d.documentHidden &&
		d.prefs.Pet.Visible && d.prefs.Notifications.Bubble && !d.prefs.Notifications.QuietHoursEnabled &&
		d.service != nil && d.service.GetState().Connection.Generation == generation
}

// dispatchConfirmedKeywordBubble 在首帧确认后显示台词，原生窗口操作前后均复核身份。
func (d *DesktopService) dispatchConfirmedKeywordBubble(plan bubblePlan, generation uint64) {
	if d.app == nil || strings.TrimSpace(plan.entry.Message) == "" {
		return
	}
	application.InvokeAsync(func() {
		d.mu.Lock()
		bubble := d.bubble
		valid := bubble != nil && d.canShowKeywordBubbleLocked(plan, generation, time.Now())
		d.mu.Unlock()
		if !valid {
			return
		}
		bubble.SetSize(300, bubbleHeight(plan.entry.Message))
		d.placeBubble(bubble)
		// 布置窗口可能触发重入；等待期间切换的动作不能由旧回调显示。
		d.mu.Lock()
		valid = d.bubble == bubble && d.canShowKeywordBubbleLocked(plan, generation, time.Now())
		d.mu.Unlock()
		if !valid {
			return
		}
		d.app.Event.Emit("pet:bubble", map[string]string{"text": plan.entry.Message, "kind": plan.entry.Kind})
		bubble.Show()
	})
}

// prepareBubbleLocked 维护唯一截止时钟，绝对时间不随轮询延长；旧回调必须验证独立代次。
func (d *DesktopService) prepareBubbleLocked(plan bubblePlan) {
	// 队列切换、隐藏或超时后清除快照，晚就绪窗口不能补取已经失效的动作。
	if d.activeKeywordTrigger != nil &&
		((plan.entry.Kind != "trigger" && plan.entry.Kind != "pickup") || plan.entry.ID != logActionID(*d.activeKeywordTrigger) ||
			d.notices.scope.Generation != d.activeKeywordTrigger.Generation || !plan.timed) {
		id := logActionID(*d.activeKeywordTrigger)
		if d.notices.center == nil || !d.notices.center.HasAction(id) {
			delete(d.keywordActions, id)
		}
		d.activeKeywordTrigger = nil
		d.keywordTriggerUntil = time.Time{}
	}
	d.bubbleVersion = plan.version
	if plan.changed {
		d.bubbleText = plan.entry.Message
		d.bubbleKind = plan.entry.Kind
		// 持久错误由前端未读历史决定，宿主仅发布本地手动错误的短时动作。
		d.manualWarning = plan.entry.Kind == "error" && strings.HasPrefix(plan.entry.ID, "manual:") && plan.entry.Message != ""
	}
	d.bubbleTimerVersion++
	timerVersion := d.bubbleTimerVersion
	if d.bubbleTimer != nil {
		d.bubbleTimer.Stop()
		d.bubbleTimer = nil
	}
	if !plan.timed || d.stopped() {
		return
	}
	d.bubbleTimer = time.AfterFunc(max(time.Duration(0), time.Until(plan.deadline)), func() {
		if d.stopped() {
			return
		}
		application.InvokeAsync(func() {
			d.mu.Lock()
			if d.stopped() || timerVersion != d.bubbleTimerVersion {
				d.mu.Unlock()
				return
			}
			next := d.notices.advance(time.Now())
			d.prepareBubbleLocked(next)
			d.mu.Unlock()
			d.dispatchBubble(next)
		})
	})
}

// logTriggerLocked 从待派发队列或当前快照查找动作，重入派发仍能识别空台词。
func (d *DesktopService) logTriggerLocked(id string) (service.LogTrigger, bool) {
	if trigger, ok := d.keywordActions[id]; ok {
		return trigger, true
	}
	if trigger := d.activeKeywordTrigger; trigger != nil && id == logActionID(*trigger) {
		return *trigger, true
	}
	return service.LogTrigger{}, false
}

// dispatchBubble 按实际工作区布置独立窗口，原生创建、隐藏和显示均在锁外执行且不抢焦点。
func (d *DesktopService) dispatchBubble(plan bubblePlan) {
	if !plan.changed {
		return
	}
	application.InvokeAsync(func() {
		d.mu.Lock()
		valid := !d.stopped() && plan.version == d.bubbleVersion
		bubble := d.bubble
		visible := !d.sleeping && d.prefs.Pet.Visible && d.prefs.Notifications.Bubble && !d.prefs.Notifications.QuietHoursEnabled && plan.entry.ID != ""
		d.mu.Unlock()
		if !valid {
			return
		}
		// 截止、拖动关闭和禁播也更新动作；不能等下次业务轮询才清除短时警告。
		d.emitPresentation()
		if !visible {
			if bubble != nil {
				bubble.Hide()
			}
			// 通知策略与窗口偏好更新可能交错；不让无法展示的动作无限占用队首。
			d.cancelUnstartedLogTrigger(plan.entry.ID)
			return
		}
		// 台词可以留空，此时仍按队列播放动作，但不创建空白气泡。
		d.mu.Lock()
		silentTrigger, silent := d.logTriggerLocked(plan.entry.ID)
		silent = silent && strings.TrimSpace(silentTrigger.Text) == "" && plan.version == d.bubbleVersion
		d.mu.Unlock()
		if silent {
			if bubble != nil {
				bubble.Hide()
			}
			if !d.emitLogTrigger(silentTrigger) {
				d.cancelUnstartedLogTrigger(plan.entry.ID)
			}
			return
		}
		if bubble == nil {
			d.mu.Lock()
			if d.bubbleCreating {
				d.mu.Unlock()
				return
			}
			d.bubbleCreating = true
			d.mu.Unlock()
			// 创建窗口可能触发生命周期回调，不能持有状态锁进入原生宿主。
			bubble = d.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "bubble", Title: "芙宁娜 · 台词", URL: "/?view=bubble", Width: 300, Height: bubbleHeight(""), Frameless: true, DisableResize: true, Hidden: true, AlwaysOnTop: true, BackgroundType: application.BackgroundTypeTransparent, IgnoreMouseEvents: true, Permissions: deniedPermissions(), Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, ExStyle: petExStyle(true), Permissions: deniedWindowsPermissions()}})
			d.mu.Lock()
			d.bubble = bubble
			d.bubbleCreating = false
			// 原生创建期间可以重入更新，必须重取最新展示意图后再显示。
			current := d.notices.advance(time.Now())
			current.changed = true
			d.prepareBubbleLocked(current)
			d.mu.Unlock()
			d.dispatchBubble(current)
			return
		}
		d.mu.Lock()
		valid = !d.stopped() && !d.sleeping && plan.version == d.bubbleVersion && d.prefs.Pet.Visible && d.prefs.Notifications.Bubble && !d.prefs.Notifications.QuietHoursEnabled
		trigger, hasTrigger := d.logTriggerLocked(plan.entry.ID)
		started := hasTrigger && d.activeKeywordTrigger != nil &&
			d.activeKeywordTrigger.ID == trigger.ID && d.activeKeywordTrigger.Generation == trigger.Generation &&
			!d.keywordTriggerUntil.IsZero()
		if started {
			plan.deadline = d.keywordTriggerUntil
			plan.timed = true
		}
		d.mu.Unlock()
		if !valid {
			return
		}
		if plan.entry.Kind == "trigger" && !hasTrigger {
			// 动作数据已不可用时跳过队首，避免等待一个无法发出的确认。
			d.cancelUnstartedLogTrigger(plan.entry.ID)
			return
		}
		// 延迟排队期间连接可能已切换，旧连接的台词与动作都不再展示。
		if hasTrigger && d.service.GetState().Connection.Generation != trigger.Generation {
			d.cancelUnstartedLogTrigger(plan.entry.ID)
			return
		}
		if hasTrigger {
			if started {
				// 已确认的重入派发只能复用原期限，不能重新发布或隐藏当前台词。
				d.dispatchConfirmedKeywordBubble(plan, trigger.Generation)
				return
			}
			// 等待首帧确认时先隐藏旧气泡并预置台词，不消耗动作的展示时间。
			bubble.Hide()
			bubble.SetSize(300, bubbleHeight(plan.entry.Message))
			d.placeBubble(bubble)
			d.mu.Lock()
			valid = d.bubble == bubble && d.canPrepareKeywordBubbleLocked(plan, trigger.Generation)
			d.mu.Unlock()
			if !valid {
				// 如动作仍在队首但已无法派发确认，按身份取消以继续后续队列。
				d.cancelUnstartedLogTrigger(plan.entry.ID)
				return
			}
			d.app.Event.Emit("pet:bubble", map[string]string{"text": plan.entry.Message, "kind": plan.entry.Kind})
			if !d.emitLogTrigger(trigger) {
				d.cancelUnstartedLogTrigger(plan.entry.ID)
			}
			return
		}
		// 台词较长时扩展气泡高度，避免固定四行裁掉用户设置的文字。
		bubble.SetSize(300, bubbleHeightForKind(plan.entry.Message, plan.entry.Kind))
		d.placeBubble(bubble)
		bubble.Show()
		d.app.Event.Emit("pet:bubble", map[string]string{"text": plan.entry.Message, "kind": plan.entry.Kind})
	})
}

// bubbleHeight 按纯台词气泡的内边距、尾部和行高估算高度，同时限制极端长文本。
func bubbleHeight(text string) int {
	return bubbleHeightForKind(text, "")
}

// bubbleHeightForKind 根据气泡类型估算高度，避免普通台词误用状态布局。
func bubbleHeightForKind(text, kind string) int {
	// 状态摘要的字段占用左列，右列按更窄的宽度估算换行。
	if kind == "status" {
		height := 39
		for _, line := range strings.Split(text, "\n") {
			value := line
			if _, after, found := strings.Cut(line, "："); found {
				value = strings.TrimSuffix(after, "】")
			}
			width := 0.0
			for _, r := range value {
				if r < 128 {
					width += 0.6
				} else {
					width++
				}
			}
			rowLines := max(1, int((width+12.9)/13))
			height += rowLines*18 + 7
		}
		return min(440, height)
	}
	lines, columns := 1, 0.0
	for _, r := range text {
		if r == '\n' {
			lines++
			columns = 0
			continue
		}
		// 英文、数字和常见标点通常比汉字窄，避免长文件名导致多余留白。
		width := 1.0
		if r < 128 {
			width = 0.6
		}
		if columns+width > 19 {
			lines++
			columns = 0
		}
		columns += width
	}
	// 预留上下边距、边框及尾巴，正文按 CSS 的 13 像素和 1.6 倍行高向上取整。
	return min(440, 47+lines*21)
}

// moveBubble 让系统纠正位置、DPI 与屏幕变化只移动气泡，不消耗当前提醒。
func (d *DesktopService) moveBubble() {
	d.moveBubbleOnScreens(d.app.Screen.GetAll())
}

// moveBubbleOnScreens 与角色纠正共享同一屏幕快照，不重复枚举半完成的布局变化。
func (d *DesktopService) moveBubbleOnScreens(screens []*application.Screen) {
	d.mu.Lock()
	bubble := d.bubble
	visible := !d.stopped() && !d.sleeping && d.prefs.Pet.Visible && d.bubbleText != ""
	d.mu.Unlock()
	if visible && bubble != nil {
		d.placeBubbleOnScreens(bubble, screens)
	}
}

// placeBubble 将气泡限制在角色所在工作区，顶部空间不足时改放到角色下方。
func (d *DesktopService) placeBubble(bubble *application.WebviewWindow) {
	d.placeBubbleOnScreens(bubble, d.app.Screen.GetAll())
}

// placeBubbleOnScreens 过滤临时无效项，并用同一工作区快照完成角色归属和气泡夹取。
func (d *DesktopService) placeBubbleOnScreens(bubble *application.WebviewWindow, screens []*application.Screen) {
	screens = monitorWorkAreas(screens)
	if len(screens) == 0 {
		return
	}
	x, y := d.pet.Position()
	w, h := d.pet.Size()
	bubbleWidth, bubbleHeight := bubble.Size()
	if bubbleWidth < 1 || bubbleHeight < 1 {
		return
	}
	_, _, screen := clampPosition(x, y, w, h, screens)
	by := y - bubbleHeight - 4
	if screen != nil && by < screen.WorkArea.Y {
		by = y + h + 4
	}
	bx, by, _ := clampPosition(x+(w-bubbleWidth)/2, by, bubbleWidth, bubbleHeight, screens)
	bubble.SetPosition(bx, by)
}

// ResetPosition 选主屏工作区右下角，支持副屏负坐标而不依赖虚拟屏总矩形。
func (d *DesktopService) ResetPosition() {
	// 即使当前没有有效屏幕，明确重置意图也先撤销旧手势。
	d.invalidatePetDrag()
	application.InvokeSync(func() {
		if d.stopped() {
			return
		}
		// 只有有效目标成功建立锚点才替换启动恢复，枚举暂失效时保留恢复快照。
		d.resetPositionOnScreens(d.app.Screen.GetAll())
	})
}

// resetPositionOnScreens 使用本次枚举中的主屏，主屏标志暂缺时采用首个有效工作区。
func (d *DesktopService) resetPositionOnScreens(screens []*application.Screen) bool {
	usable := monitorWorkAreas(screens)
	if len(usable) == 0 {
		return false
	}
	target := usable[0]
	for _, screen := range usable {
		if screen.IsPrimary {
			target = screen
			break
		}
	}
	w, h := d.pet.Size()
	a := target.WorkArea
	return d.anchorPosition(a.X+max(0, a.Width-w-36), a.Y+max(0, a.Height-h-28), w, h, target, screens)
}

// restorePosition 只消费建窗前已锁存的启动意图，迟到就绪消息不能覆盖用户已经重置或拖动的位置。
func (d *DesktopService) restorePosition() {
	application.InvokeSync(func() {
		if d.stopped() {
			return
		}
		d.restorePendingPosition(d.app.Screen.GetAll())
	})
}

// restorePendingPosition 返回是否存在恢复意图及是否完成，原屏仅暂失效时不回退或覆盖磁盘。
func (d *DesktopService) restorePendingPosition(screens []*application.Screen) (bool, bool) {
	d.mu.Lock()
	var p savedPosition
	resetPending := d.positionResetPending
	restorePending := d.positionRestore != nil
	if restorePending {
		p = *d.positionRestore
	}
	d.mu.Unlock()
	if !restorePending {
		// 无历史位置也保留默认重置意图，空枚举或无效尺寸不能提前把它消费掉。
		if resetPending {
			return true, d.resetPositionOnScreens(screens)
		}
		return false, false
	}
	for _, screen := range screens {
		if screen != nil && screen.ID == p.Monitor {
			w, h := d.pet.Size()
			return true, d.anchorPosition(screen.WorkArea.X+p.RelativeX, screen.WorkArea.Y+p.RelativeY, w, h, screen, screens)
		}
	}
	return true, d.resetPositionOnScreens(screens)
}

// anchorPosition 在原生移动前替换锚点，并直接保存已夹取位置，不依赖异步移动事件落盘。
func (d *DesktopService) anchorPosition(x, y, w, h int, target *application.Screen, screens []*application.Screen) bool {
	d.mu.Lock()
	cx, cy, ok := d.monitor.anchor(x, y, w, h, target)
	if ok {
		d.positionRestore = nil
		d.positionResetPending = false
	}
	position := d.positionSnapshotLocked(cx, cy)
	d.mu.Unlock()
	if !ok {
		return false
	}
	d.pet.SetPosition(cx, cy)
	savePosition(position)
	d.moveBubbleOnScreens(screens)
	return true
}

// correctPosition 将原生读取、纯状态决策和实际移动放在一个 UI 事务中，保留合法负坐标。
func (d *DesktopService) correctPosition() {
	application.InvokeSync(func() {
		if !d.stopped() {
			d.correctPositionOnScreens(d.app.Screen.GetAll())
		}
	})
}

// correctPendingPosition 仅消费未完成布局变化，普通边缘拖动不会被逐帧全窗夹回。
func (d *DesktopService) correctPendingPosition(screens []*application.Screen) bool {
	d.mu.Lock()
	pending := d.positionRestore != nil || d.positionResetPending || d.monitor.pending(screens)
	d.mu.Unlock()
	return !pending || d.correctPositionOnScreens(screens)
}

// correctPositionOnScreens 由 UI 队列调用，屏幕签名、状态迁移和持久化共用调用方的枚举快照。
func (d *DesktopService) correctPositionOnScreens(screens []*application.Screen) bool {
	if pending, ok := d.restorePendingPosition(screens); pending {
		return ok
	}
	x, y := d.pet.Position()
	w, h := d.pet.Size()
	d.mu.Lock()
	cx, cy, ok := d.monitor.correct(x, y, w, h, screens)
	if ok && (cx != x || cy != y) && d.drag.token != "" {
		// 显式显示或校正已经改变实际原点，不能继续沿用手势开始时的旧绝对坐标。
		d.drag.invalidate()
	}
	position := d.positionSnapshotLocked(cx, cy)
	d.mu.Unlock()
	if ok && (cx != x || cy != y) {
		d.pet.SetPosition(cx, cy)
	}
	if ok {
		savePosition(position)
		d.moveBubbleOnScreens(screens)
	}
	return ok
}

// positionSnapshotLocked 仅复制已接受的逻辑位置，由调用方持锁，不包含任何原生或文件调用。
func (d *DesktopService) positionSnapshotLocked(x, y int) savedPosition {
	return savedPosition{
		Version: 1, Monitor: d.monitor.monitorID, X: x, Y: y,
		RelativeX: d.monitor.relativeX, RelativeY: d.monitor.relativeY,
	}
}

// rememberPosition 的输入来自本程序原生窗口，退出阶段仍可保存，前端不能指定文件或屏幕路径。
func (d *DesktopService) rememberPosition() {
	application.InvokeSync(func() {
		x, y := d.pet.Position()
		w, h := d.pet.Size()
		screens := d.app.Screen.GetAll()
		d.mu.Lock()
		// 未消费的启动恢复和布局迁移均不能被早到移动消息或退出保存覆盖。
		accepted := d.positionRestore == nil && !d.positionResetPending && d.monitor.remember(x, y, w, h, screens)
		position := d.positionSnapshotLocked(x, y)
		d.mu.Unlock()
		if accepted {
			savePosition(position)
		}
	})
}

// watchScreens 只在显示器布局变化时纠正位置；退出时停止定时器，不持续干扰拖动。
func (d *DesktopService) watchScreens() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			application.InvokeAsync(func() {
				if d.stopped() {
					return
				}
				screens := d.app.Screen.GetAll()
				signature := monitorLayoutSignature(screens)
				environment := d.petDragEnvironmentOnScreens(screens)
				d.mu.Lock()
				// 按会话自身环境比较，首次空缓存签名不会误取消刚开始的有效手势。
				d.drag.observe(environment)
				pending := d.positionRestore != nil || d.positionResetPending || d.monitor.pending(screens)
				d.mu.Unlock()
				if signature != d.monitorSignature || pending {
					// 失败时不消费签名，窗口尺寸或工作区恢复可用后仍会再次尝试。
					if d.correctPositionOnScreens(screens) {
						d.monitorSignature = signature
					}
				}
			})
		}
	}
}

// Shutdown 只供宿主退出使用，停止显示器监视和气泡计时器。
//
//wails:ignore
func (d *DesktopService) Shutdown() {
	d.once.Do(func() { close(d.done) })
	// 网络通知句柄由工作协程在退出时注销，避免保留指向已关闭宿主的系统回调。
	d.networkWorker.Wait()
	d.mu.Lock()
	d.drag.invalidate()
	d.bubbleVersion++
	d.bubbleTimerVersion++
	if d.bubbleTimer != nil {
		d.bubbleTimer.Stop()
		d.bubbleTimer = nil
	}
	if d.faintTimer != nil {
		d.faintTimer.Stop()
		d.faintTimer = nil
	}
	d.mu.Unlock()
	d.rememberPosition()
}

// Quit 才真正结束程序；隐藏和关闭面板均不调用它。
func (d *DesktopService) Quit() { d.app.Quit() }
