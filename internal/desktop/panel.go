package desktop

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// HandleHostWarning 只消费已锁定 Wails 版本明确报告的托盘创建失败，启动前先锁存。
//
//wails:ignore
func (d *DesktopService) HandleHostWarning(warning string) {
	if d.stopped() || !strings.HasPrefix(warning, "initial systray add failed:") {
		return
	}
	d.mu.Lock()
	d.trayWarning = "系统托盘创建失败，已显示控制台；可从控制台退出，或重新启动桌宠恢复入口"
	queued := d.panelLifecycle.trayFailed()
	started := d.panelLifecycle.started
	d.mu.Unlock()
	if queued {
		d.queuePanel()
	}
	if started {
		application.InvokeAsync(func() {
			if !d.stopped() {
				d.emitPresentation()
			}
		})
	}
}

// RestoreFromHost 将快捷键或第二次启动的恢复请求锁存到 UI 队列，允许早于宿主启动。
//
//wails:ignore
func (d *DesktopService) RestoreFromHost() {
	if d.stopped() {
		return
	}
	d.mu.Lock()
	d.restorePetPending = true
	queued := d.panelLifecycle.request(true, true)
	d.mu.Unlock()
	if queued {
		d.queuePanel()
	}
}

// startDesktop 只启动一次监视与唯一业务连接，托盘初始化早到或迟到均可触发兜底。
func (d *DesktopService) startDesktop(needsSetup bool, shortcut string) {
	if d.stopped() {
		return
	}
	d.mu.Lock()
	if d.panelLifecycle.started {
		d.mu.Unlock()
		return
	}
	queued := d.panelLifecycle.start(needsSetup)
	d.mu.Unlock()
	if queued {
		d.queuePanel()
	}
	application.InvokeAsync(func() {
		if !d.stopped() {
			d.updateShortcut(shortcut)
			d.emitPresentation()
		}
	})
	go d.watchScreens()
	d.service.AutoConnect()
}

// requestPanel 不触碰原生窗口，绑定、托盘和窗口关闭回调都使用同一条展示意图通道。
func (d *DesktopService) requestPanel(visible, focus bool) {
	if d.stopped() {
		return
	}
	d.mu.Lock()
	queued := d.panelLifecycle.request(visible, focus)
	d.mu.Unlock()
	if queued {
		d.queuePanel()
	}
}

// ShowStatus 让原生查看状态入口定位概览；反复请求仍应覆盖用户后来切换的页签。
func (d *DesktopService) ShowStatus() { d.requestPanelDestination("overview", "") }

// ShowConnectionSettings 直接打开连接管理，不携带地址、凭据或任意导航参数。
func (d *DesktopService) ShowConnectionSettings() { d.requestPanelDestination("connections", "") }

// ShowNotificationHistory 打开设置页的通知记录，阅读动作仍由用户单独确认。
func (d *DesktopService) ShowNotificationHistory() {
	d.requestPanelDestination("settings", "notifications")
}

// requestPanelDestination 原子锁存导航和展示版本，所有原生操作与广播均在状态锁外。
func (d *DesktopService) requestPanelDestination(section, target string) {
	if d.stopped() {
		return
	}
	d.mu.Lock()
	if !d.panelNavigation.request(section, target) {
		d.mu.Unlock()
		return
	}
	queued := d.panelLifecycle.request(true, true)
	started := d.panelLifecycle.started
	d.presentationRevision++
	snapshot := d.presentationLocked()
	d.mu.Unlock()
	if started && !d.stopped() {
		// 面板已经可见也要发布导航，不能依赖显隐变化触发。
		d.app.Event.Emit("pet:presentation", snapshot)
	}
	if queued {
		d.queuePanel()
	}
}

// queuePanel 只在应用已启动后调用；所有面板创建、显示、聚焦和隐藏都在 UI 线程执行。
func (d *DesktopService) queuePanel() {
	application.InvokeAsync(d.applyPanelIntent)
}

// applyPanelIntent 不持宿主锁进入原生调用，防止 WebView 初始化的嵌套消息泵重入死锁。
func (d *DesktopService) applyPanelIntent() {
	d.mu.Lock()
	if d.stopped() || !d.panelLifecycle.begin() {
		d.mu.Unlock()
		return
	}
	restorePet := d.restorePetPending
	d.restorePetPending = false
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		queued := d.panelLifecycle.finish()
		d.mu.Unlock()
		if queued && !d.stopped() {
			d.queuePanel()
		}
	}()
	if restorePet && !d.stopped() {
		d.ShowPet()
		d.SetClickThrough(false)
	}
	var intent panelIntent
	for !d.stopped() {
		d.mu.Lock()
		create := d.panelLifecycle.visible && d.panel == nil
		if !create {
			intent = d.panelLifecycle.take()
		}
		d.mu.Unlock()
		if !create {
			break
		}
		d.createPanel()
		// 原生创建期间可能收到关闭或再次打开，创建返回后重新读取最后意图。
	}
	if d.stopped() {
		return
	}
	if d.panel == nil {
		d.updatePanelVisibility(false)
		return
	}
	if intent.visible {
		d.panel.Show()
		d.mu.Lock()
		focus := d.panelLifecycle.mayFocus(intent)
		d.mu.Unlock()
		if focus && !d.stopped() {
			d.panel.Focus()
		}
	} else {
		d.panel.Hide()
	}
	if !d.stopped() {
		d.updatePanelVisibility(d.panel.IsVisible())
	}
}

// createPanel 先登记唯一 Go 对象和关闭钩子，再运行原生宿主，创建重入不会产生第二个面板。
func (d *DesktopService) createPanel() {
	panel := application.NewWindow(application.WebviewWindowOptions{
		Name: "panel", Title: "芙宁娜桌宠 · 设置", URL: "/?view=panel", Width: 1060, Height: 760, MinWidth: 700, MinHeight: 560,
		BackgroundColour: application.NewRGB(239, 246, 255), Hidden: true,
		DefaultContextMenuDisabled: true,
		Permissions:                deniedPermissions(),
		Windows:                    application.WindowsWindow{Permissions: deniedWindowsPermissions()},
	})
	d.panel = panel
	panel.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		d.HidePanel()
	})
	// 窗口事件可能异步迟到，读取当前可见性而非使用旧事件覆盖采集频率。
	panel.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) { d.syncPanelVisibility() })
	panel.OnWindowEvent(events.Common.WindowHide, func(*application.WindowEvent) { d.syncPanelVisibility() })
	d.app.Window.Add(panel)
	panel.Run()
}

// syncPanelVisibility 读取当前原生事实，迟到的显示事件不能重新启动已隐藏面板的计时器。
func (d *DesktopService) syncPanelVisibility() {
	if d.stopped() {
		return
	}
	application.InvokeAsync(func() {
		if !d.stopped() && d.panel != nil {
			d.updatePanelVisibility(d.panel.IsVisible())
		}
	})
}

// updatePanelVisibility 同步唯一采集器与前端展示；关闭仍默认隐藏，保留未保存的编辑内容。
func (d *DesktopService) updatePanelVisibility(visible bool) {
	d.service.SetPanelVisible(visible)
	d.mu.Lock()
	changed := d.panelVisible != visible
	d.panelVisible = visible
	d.mu.Unlock()
	if changed {
		d.emitPresentation()
	}
}

// hostWarningLocked 合并互不覆盖的恢复告警，调用方必须持有宿主锁。
func (d *DesktopService) hostWarningLocked(shortcutWarning string) string {
	warnings := make([]string, 0, 3)
	// 快捷键状态只清理自身提示，托盘或网络故障不能被一次成功重试覆盖。
	for _, warning := range []string{d.trayWarning, shortcutWarning, d.networkWarning} {
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return strings.Join(warnings, "；")
}
