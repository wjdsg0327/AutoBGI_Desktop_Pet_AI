package desktop

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// todoAllowedLocked 与角色的可见、休眠和鼠标穿透状态保持一致。
func (d *DesktopService) todoAllowedLocked() bool {
	return !d.stopped() && !d.sleeping && !d.powerSleepingIntent && !d.documentHidden &&
		d.prefs.Pet.Visible && !d.prefs.Pet.ClickThrough
}

// SetTodoPetHover 在连续悬停三秒后显示圆圈，离开时为跨窗口移动保留缓冲。
func (d *DesktopService) SetTodoPetHover(hover bool) {
	d.mu.Lock()
	if !d.todoAllowedLocked() {
		d.mu.Unlock()
		return
	}
	d.todoPetHover = hover
	d.todoHoverRevision++
	revision := d.todoHoverRevision
	if d.todoShowTimer != nil {
		d.todoShowTimer.Stop()
		d.todoShowTimer = nil
	}
	if d.todoHideTimer != nil {
		d.todoHideTimer.Stop()
		d.todoHideTimer = nil
	}
	if hover && !d.todoVisible {
		d.todoShowTimer = time.AfterFunc(3*time.Second, func() {
			application.InvokeAsync(func() { d.showTodoCircleOnUI(revision) })
		})
	} else if !hover && d.todoVisible && !d.todoExpanded && !d.todoWindowHover {
		d.scheduleTodoHideLocked()
	}
	d.mu.Unlock()
}

// SetTodoWindowHover 防止鼠标从角色移向圆圈时窗口提前消失。
func (d *DesktopService) SetTodoWindowHover(hover bool) {
	d.mu.Lock()
	if !d.todoAllowedLocked() || !d.todoVisible {
		d.mu.Unlock()
		return
	}
	d.todoWindowHover = hover
	if d.todoHideTimer != nil {
		d.todoHideTimer.Stop()
		d.todoHideTimer = nil
	}
	if !hover && !d.todoPetHover && !d.todoExpanded {
		d.scheduleTodoHideLocked()
	}
	d.mu.Unlock()
}

// scheduleTodoHideLocked 使用独立代次拒绝已经失效的延迟隐藏回调。
func (d *DesktopService) scheduleTodoHideLocked() {
	d.todoHoverRevision++
	revision := d.todoHoverRevision
	d.todoHideTimer = time.AfterFunc(600*time.Millisecond, func() {
		application.InvokeAsync(func() {
			d.mu.Lock()
			hide := d.todoHoverRevision == revision && !d.todoPetHover && !d.todoWindowHover && !d.todoExpanded
			d.mu.Unlock()
			if hide {
				d.hideTodoOnUI()
			}
		})
	})
}

// showTodoCircleOnUI 在原生 UI 队列创建窗口，并在每次原生调用前后复核悬停状态。
func (d *DesktopService) showTodoCircleOnUI(revision uint64) {
	d.mu.Lock()
	valid := d.todoAllowedLocked() && d.todoPetHover && d.todoHoverRevision == revision && !d.todoVisible
	window := d.todoWindow
	if valid && window == nil && !d.todoCreating {
		d.todoCreating = true
	} else if window == nil {
		valid = false
	}
	onTop := d.prefs.Pet.AlwaysOnTop
	d.mu.Unlock()
	if !valid {
		return
	}
	if window == nil {
		window = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name: "todo", Title: "芙宁娜 · 代办", URL: "/?view=todo", Width: 76, Height: 76,
			Frameless: true, DisableResize: true, Hidden: true, AlwaysOnTop: onTop,
			BackgroundType: application.BackgroundTypeTransparent, DefaultContextMenuDisabled: true,
			Permissions: deniedPermissions(),
			Windows:     application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, ExStyle: petExStyle(onTop), Permissions: deniedWindowsPermissions()},
		})
		d.mu.Lock()
		d.todoWindow, d.todoCreating = window, false
		d.mu.Unlock()
	}
	d.mu.Lock()
	valid = d.todoAllowedLocked() && d.todoPetHover && d.todoHoverRevision == revision
	if valid {
		d.todoVisible, d.todoExpanded = true, false
		d.todoViewRevision++
	}
	revisionState := d.todoViewRevision
	d.mu.Unlock()
	if !valid {
		return
	}
	window.SetSize(76, 76)
	window.SetAlwaysOnTop(onTop)
	d.moveTodoOnScreens(d.app.Screen.GetAll())
	d.app.Event.Emit("pet:todo-window", map[string]any{"visible": true, "expanded": false, "revision": revisionState})
	window.Show()
}

// OpenTodoWindow 展开可点击的对话，保留待办直到用户选择处理或不管。
func (d *DesktopService) OpenTodoWindow() {
	application.InvokeAsync(func() {
		d.mu.Lock()
		window := d.todoWindow
		valid := d.todoAllowedLocked() && d.todoVisible && window != nil
		if valid {
			d.todoExpanded = true
			d.todoViewRevision++
			if d.todoHideTimer != nil {
				d.todoHideTimer.Stop()
				d.todoHideTimer = nil
			}
		}
		revisionState := d.todoViewRevision
		d.mu.Unlock()
		if !valid {
			return
		}
		window.SetSize(340, 330)
		d.moveTodoOnScreens(d.app.Screen.GetAll())
		d.app.Event.Emit("pet:todo-window", map[string]any{"visible": true, "expanded": true, "revision": revisionState})
	})
}

// GetTodoWindowState 在新窗口错过广播时补取实际展示状态。
func (d *DesktopService) GetTodoWindowState() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return map[string]any{"visible": d.todoVisible, "expanded": d.todoExpanded, "revision": d.todoViewRevision}
}

// HideTodoWindow 收起窗口并撤销所有旧悬停计时，不删除代办。
func (d *DesktopService) HideTodoWindow() {
	if !d.stopped() {
		application.InvokeAsync(d.hideTodoOnUI)
	}
}

// hideTodoOnUI 在释放状态锁后隐藏原生窗口，避免窗口回调重入死锁。
func (d *DesktopService) hideTodoOnUI() {
	d.mu.Lock()
	d.todoHoverRevision++
	d.todoPetHover, d.todoWindowHover = false, false
	d.todoVisible, d.todoExpanded = false, false
	d.todoViewRevision++
	revisionState := d.todoViewRevision
	if d.todoShowTimer != nil {
		d.todoShowTimer.Stop()
		d.todoShowTimer = nil
	}
	if d.todoHideTimer != nil {
		d.todoHideTimer.Stop()
		d.todoHideTimer = nil
	}
	window := d.todoWindow
	d.mu.Unlock()
	if window != nil {
		window.Hide()
	}
	if d.app != nil && !d.stopped() {
		d.app.Event.Emit("pet:todo-window", map[string]any{"visible": false, "expanded": false, "revision": revisionState})
	}
}

// syncTodoWindowOnUI 在偏好变化时同步置顶，隐藏或穿透时立即关闭交互窗口。
func (d *DesktopService) syncTodoWindowOnUI() {
	d.mu.Lock()
	allowed, visible, window, onTop := d.todoAllowedLocked(), d.todoVisible, d.todoWindow, d.prefs.Pet.AlwaysOnTop
	d.mu.Unlock()
	if !allowed {
		d.hideTodoOnUI()
		return
	}
	if visible && window != nil {
		window.SetAlwaysOnTop(onTop)
	}
}

// moveTodoOnScreens 优先放在角色右侧，空间不足时切换到左侧并夹入当前屏幕工作区。
func (d *DesktopService) moveTodoOnScreens(screens []*application.Screen) {
	d.mu.Lock()
	window, visible := d.todoWindow, d.todoVisible && d.todoAllowedLocked()
	d.mu.Unlock()
	screens = monitorWorkAreas(screens)
	if !visible || window == nil || d.pet == nil || len(screens) == 0 {
		return
	}
	x, y := d.pet.Position()
	width, height := d.pet.Size()
	todoWidth, todoHeight := window.Size()
	if todoWidth < 1 || todoHeight < 1 {
		return
	}
	_, _, screen := clampPosition(x, y, width, height, screens)
	if screen == nil {
		return
	}
	tx, ty := todoPosition(x, y, width, height, todoWidth, todoHeight, screen.WorkArea)
	window.SetPosition(tx, ty)
}

// todoPosition 在角色所在工作区选取右侧或左侧，并确保窗口始终落在可见区域。
func todoPosition(x, y, width, height, todoWidth, todoHeight int, area application.Rect) (int, int) {
	tx := x + width - 8
	if tx+todoWidth > area.X+area.Width {
		tx = x - todoWidth + 8
	}
	ty := y + height/5
	tx = max(area.X, min(tx, area.X+max(0, area.Width-todoWidth)))
	ty = max(area.Y, min(ty, area.Y+max(0, area.Height-todoHeight)))
	return tx, ty
}
