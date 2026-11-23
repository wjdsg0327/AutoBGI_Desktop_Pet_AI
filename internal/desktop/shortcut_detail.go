package desktop

import (
	"fmt"
	"strings"
	"time"

	"autobgi-pet/internal/notification"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const shortcutDetailDuration = 3 * time.Second

// TriggerShortcutDetail 是全局快捷键的唯一回调入口，异步切换到宿主 UI 队列。
func (d *DesktopService) TriggerShortcutDetail() {
	if d.stopped() {
		return
	}
	application.InvokeAsync(d.beginShortcutDetail)
}

// beginShortcutDetail 创建一次临时展示会话，不修改用户保存的桌宠可见偏好。
func (d *DesktopService) beginShortcutDetail() {
	if d.stopped() || d.app == nil || d.pet == nil {
		return
	}
	d.mu.Lock()
	if d.stopped() {
		d.mu.Unlock()
		return
	}
	d.shortcutDetailVersion++
	version := d.shortcutDetailVersion
	d.shortcutDetailVisible = true
	if d.shortcutDetailTimer != nil {
		d.shortcutDetailTimer.Stop()
		d.shortcutDetailTimer = nil
	}
	wasHidden := !d.prefs.Pet.Visible
	generation := d.service.GetState().Connection.Generation
	d.mu.Unlock()

	if wasHidden {
		d.pet.Show()
		d.correctPosition()
	}
	d.emitPresentation()
	go d.queryShortcutDetail(version, generation)
}

// queryShortcutDetail 复用现有 BGI 状态查询，不阻塞快捷键回调或宿主 UI 线程。
func (d *DesktopService) queryShortcutDetail(version, generation uint64) {
	text, err := d.service.QueryBGIStatus()
	kind := "status"
	if err != nil {
		text = err.Error()
		kind = "info"
	}
	application.InvokeAsync(func() {
		d.commitShortcutDetail(version, generation, text, kind)
	})
}

// commitShortcutDetail 将状态结果加入通知中心，并以三秒作为本条通知的展示时长。
func (d *DesktopService) commitShortcutDetail(version, generation uint64, text, kind string) {
	now := time.Now()
	d.mu.Lock()
	state := d.service.GetState()
	if d.stopped() || version != d.shortcutDetailVersion || !d.shortcutDetailVisible {
		d.mu.Unlock()
		return
	}
	if state.Connection.Generation != generation {
		// 查询期间连接已切换，展示明确提示并继续当前会话，确保隐藏桌宠仍按三秒完成收尾。
		text = "连接已变化，请重新点击查看"
		kind = "info"
	}
	// 每次触发都刷新通知策略，确保隐藏状态下创建的通知中心立即接受临时展示。
	_ = d.notices.update(d.visibleNotificationStateLocked(state), d.sleeping, now)
	d.manualNoticeSequence++
	entry := notification.Entry{
		ID:        fmt.Sprintf("shortcut:%d", d.manualNoticeSequence),
		Kind:      kind,
		Message:   text,
		CreatedAt: now.Format(time.RFC3339Nano),
		Duration:  shortcutDetailDuration,
		// 快捷键是即时查看入口，覆盖当前普通气泡后仍按通知中心截止时间计时。
		Priority: 6,
	}
	plan := d.notices.offer(entry, now)
	d.prepareBubbleLocked(plan)
	d.mu.Unlock()

	d.dispatchBubble(plan)
}

// armShortcutDetailTimerLocked 让当前快捷键气泡从通知中心实际激活的时刻计时三秒。
// 调用方必须持有桌面锁；查询结果未进入气泡时不提前结束临时显示。
func (d *DesktopService) armShortcutDetailTimerLocked(plan bubblePlan) {
	if !d.shortcutDetailVisible || !strings.HasPrefix(plan.entry.ID, "shortcut:") || !plan.timed {
		return
	}
	version := d.shortcutDetailVersion
	if d.shortcutDetailTimer != nil {
		d.shortcutDetailTimer.Stop()
	}
	delay := time.Until(plan.deadline)
	if delay < 0 {
		delay = 0
	}
	d.shortcutDetailTimer = time.AfterFunc(delay, func() {
		application.InvokeAsync(func() {
			d.finishShortcutDetail(version)
		})
	})
}

// finishShortcutDetail 只恢复仍属于本次会话的临时隐藏状态，避免覆盖用户后续操作。
func (d *DesktopService) finishShortcutDetail(version uint64) {
	var plan bubblePlan
	d.mu.Lock()
	if d.stopped() || version != d.shortcutDetailVersion || !d.shortcutDetailVisible {
		d.mu.Unlock()
		return
	}
	d.shortcutDetailVersion++
	d.shortcutDetailVisible = false
	d.shortcutDetailTimer = nil
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
	d.shortcutDetailVersion++
	d.shortcutDetailVisible = false
	if d.shortcutDetailTimer != nil {
		d.shortcutDetailTimer.Stop()
		d.shortcutDetailTimer = nil
	}
	d.mu.Unlock()
}
