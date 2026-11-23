package desktop

import (
	"context"
	"fmt"
	"time"

	"autobgi-pet/internal/notification"
	"autobgi-pet/internal/service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const shortcutDetailDuration = 3 * time.Second

// TriggerShortcutDetail 是全局快捷键的唯一回调入口，异步切换到宿主 UI 队列。
func (d *DesktopService) TriggerShortcutDetail() {
	if d.stopped() {
		return
	}
	application.InvokeAsync(func() { d.beginPetDetail(true) })
}

// beginPetDetail 让单击与快捷键共用一次可取消的状态和截图查询。
func (d *DesktopService) beginPetDetail(shortcut bool) {
	if d.stopped() || d.app == nil || d.pet == nil {
		return
	}
	d.mu.Lock()
	if d.stopped() || d.sleeping || d.powerSleepingIntent || !d.prefs.Notifications.Bubble ||
		d.prefs.Notifications.QuietHoursEnabled || (!shortcut && !d.presentationVisibleLocked()) {
		d.mu.Unlock()
		return
	}
	// 在临时显示的桌宠上再次点击，同样在新详情展示完毕后恢复隐藏。
	reveal := shortcut && !d.petVisibleLocked()
	temporary := shortcut || d.shortcutDetailVisible && !d.prefs.Pet.Visible
	d.clearPetDetailLocked()
	d.shortcutDetailVisible = temporary
	d.manualNoticeSequence++
	prefix := "detail"
	if shortcut {
		prefix = "shortcut"
	}
	id := fmt.Sprintf("%s:%d", prefix, d.manualNoticeSequence)
	ctx, cancel := context.WithCancel(context.Background())
	d.detailID, d.detailCancel = id, cancel
	scope := notificationScope(d.service.GetState())
	d.mu.Unlock()

	// 两个详情入口都先收起代办圆圈或对话，并作废本次悬停计时，再开始取图。
	d.hideTodoOnUI()
	if reveal {
		d.pet.Show()
		d.correctPosition()
	}
	if temporary {
		d.emitPresentation()
	}
	// UI 回调只提交仍属于当前请求的结果，旧连接和已取消的请求都不会发布图片。
	go func() {
		result := d.service.QueryPetDetail(ctx, scope.Generation)
		if ctx.Err() == nil && !d.stopped() {
			application.InvokeAsync(func() { d.commitPetDetail(id, scope, shortcut, result) })
		}
	}()
}

// commitPetDetail 将独立成败的状态与截图绑定到同一条通知，沿用各入口的优先级和时长。
func (d *DesktopService) commitPetDetail(id string, scope notification.Scope, shortcut bool, result service.PetDetailResult) {
	now := time.Now()
	d.mu.Lock()
	state := d.service.GetState()
	if d.stopped() || id != d.detailID || d.detailCancel == nil {
		d.mu.Unlock()
		return
	}
	d.detailCancel()
	d.detailCancel = nil
	if notificationScope(state) != scope {
		// 切换期间的单击直接作废，快捷键继续显示提示并按时恢复隐藏。
		if !d.shortcutDetailVisible {
			d.mu.Unlock()
			return
		}
		result = service.PetDetailResult{Text: "连接已变化，请重新点击查看"}
	}
	// 刷新范围和显隐策略，保留策略先行清除旧气泡产生的重绘请求。
	updated := d.notices.update(d.visibleNotificationStateLocked(state), d.sleeping, now)
	entry := notification.Entry{
		ID: id, Kind: "manual", Message: result.Text,
		Screenshot: result.JPEG, ScreenshotFailed: result.ScreenshotFailed,
		CreatedAt: now.Format(time.RFC3339Nano),
	}
	if result.StatusOK {
		entry.Kind = "status"
	}
	if shortcut {
		entry.Duration = shortcutDetailDuration
		entry.Priority = 6
	}
	plan := d.notices.offer(entry, now)
	plan.changed = plan.changed || updated.changed
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()

	d.dispatchBubble(plan)
}

// armShortcutDetailTimerLocked 按当前详情的展示期限恢复临时显示的桌宠。
// 调用方必须持有桌面锁；查询结果未进入气泡时不提前结束临时显示。
func (d *DesktopService) armShortcutDetailTimerLocked(plan bubblePlan) {
	if !d.shortcutDetailVisible || d.detailCancel != nil {
		return
	}
	// 被禁播或被清出队列时也结束临时会话，等待排队的详情则从实际展示时计时。
	active := plan.entry.ID == d.detailID && plan.timed
	if !active && (d.shortcutDetailTimer != nil || d.notices.center.HasAction(d.detailID)) {
		return
	}
	id := d.detailID
	if d.shortcutDetailTimer != nil {
		d.shortcutDetailTimer.Stop()
	}
	var delay time.Duration
	if active {
		delay = time.Until(plan.deadline)
	}
	d.shortcutDetailTimer = time.AfterFunc(delay, func() {
		application.InvokeAsync(func() {
			d.finishShortcutDetail(id)
		})
	})
}

// finishShortcutDetail 只恢复仍属于本次会话的临时隐藏状态，避免覆盖用户后续操作。
func (d *DesktopService) finishShortcutDetail(id string) {
	var plan bubblePlan
	d.mu.Lock()
	if d.stopped() || id != d.detailID || !d.shortcutDetailVisible {
		d.mu.Unlock()
		return
	}
	d.clearPetDetailLocked()
	shouldHide := !d.prefs.Pet.Visible
	if shouldHide {
		// 临时会话结束时同步通知中心的隐藏策略，确保气泡不会晚于桌宠继续残留。
		plan = d.notices.update(d.notificationStateLocked(), d.sleeping, time.Now())
		d.prepareBubbleLocked(plan)
	}
	d.mu.Unlock()

	if shouldHide && d.pet != nil {
		d.pet.Hide()
		d.HideTodoWindow()
	}
	d.emitPresentation()
	if shouldHide {
		d.dispatchBubble(plan)
	}
}

// cancelShortcutDetail 让用户主动显示或隐藏桌宠时立即接管可见状态。
func (d *DesktopService) cancelShortcutDetail() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.clearPetDetailLocked()
}

// clearPetDetailLocked 撤销读取、展示计时和请求身份，使所有已排队的旧回调失效。
func (d *DesktopService) clearPetDetailLocked() {
	d.detailID = ""
	if d.detailCancel != nil {
		d.detailCancel()
		d.detailCancel = nil
	}
	d.shortcutDetailVisible = false
	if d.shortcutDetailTimer != nil {
		d.shortcutDetailTimer.Stop()
		d.shortcutDetailTimer = nil
	}
}
