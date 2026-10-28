package desktop

import (
	"time"

	"autobgi-pet/internal/notification"
	"autobgi-pet/internal/service"
)

// bubblePlan 将队列进度与原生窗口分离；重复快照不能延长当前提醒的绝对截止时间。
type bubblePlan struct {
	version  uint64
	changed  bool
	entry    notification.Entry
	deadline time.Time
	timed    bool
}

// bubbleState 由桌面宿主锁保护，拖动只抑制当前气泡，后续提醒仍按原队列推进。
type bubbleState struct {
	center     *notification.Center
	scope      notification.Scope
	entryID    string
	version    uint64
	suppressed bool
}

// update 仅摄入本进程已持久保留展示尝试的提醒，恢复的历史永远不进入气泡队列。
func (b *bubbleState) update(state service.AppState, sleeping bool, now time.Time) bubblePlan {
	if b.center == nil {
		b.center = notification.New()
	}
	scope := notificationScope(state)
	if scope.Generation < b.scope.Generation {
		return b.advance(now)
	}
	b.scope = scope
	entries := make([]notification.Entry, 0, len(state.Notifications))
	for _, item := range state.Notifications {
		entries = append(entries, notification.Entry{ID: item.ID, Kind: item.Kind, Message: item.Message, CreatedAt: item.CreatedAt})
	}
	b.center.Update(b.scope, entries, notification.Policy{
		Hidden: !state.Preferences.Pet.Visible, Sleeping: sleeping,
		BubbleDisabled: !state.Preferences.Notifications.Bubble, DoNotDisturb: state.Preferences.Notifications.QuietHoursEnabled,
		QuietManaged: true, // 未读摘要由业务日志原子提交，展示层不能再生成第二条摘要。
	}, now)
	return b.advance(now)
}

// notificationScope 使用连接身份，历史快照只在资料一致时作为实例身份的后备来源。
func notificationScope(state service.AppState) notification.Scope {
	scope := notification.Scope{ProfileID: state.Connection.ProfileID, Generation: state.Connection.Generation}
	if scope.ProfileID == "" {
		scope.ProfileID = state.ActiveProfileID
	}
	if state.Capabilities != nil && state.Capabilities.InstanceID != nil {
		scope.InstanceID = *state.Capabilities.InstanceID
	} else if state.Snapshot != nil && state.Snapshot.ProfileID == scope.ProfileID && state.Snapshot.ServerInstanceID != nil {
		scope.InstanceID = *state.Snapshot.ServerInstanceID
	}
	return scope
}

// offer 让手动查询遵守同一优先级与勿扰策略，不直接覆盖错误或命令结果。
func (b *bubbleState) offer(entry notification.Entry, now time.Time) bubblePlan {
	if b.center == nil {
		b.center = notification.New()
	}
	b.center.OfferManual(b.scope, entry, now)
	return b.advance(now)
}

// offerTrigger 保留每次关键词命中的身份，供宿主在对应台词展示时播放动作。
func (b *bubbleState) offerTrigger(entry notification.Entry, now time.Time) (bubblePlan, bool, bool) {
	if b.center == nil {
		b.center = notification.New()
	}
	accepted := b.center.OfferManual(b.scope, entry, now)
	return b.advance(now), accepted, !accepted && b.center.TriggerQueueFull()
}

// offerPickup 将拾取放入独立保护的队列，并与关键词动作统一排序。
func (b *bubbleState) offerPickup(entry notification.Entry, now time.Time) (bubblePlan, bool) {
	if b.center == nil {
		b.center = notification.New()
	}
	accepted := b.center.OfferPickup(b.scope, entry, now)
	return b.advance(now), accepted
}

// awaitTrigger 在发布待展示动作时启动确认期限，重复发布不延长等待时间。
func (b *bubbleState) awaitTrigger(id string, now time.Time) (bubblePlan, bool) {
	if b.center == nil {
		return bubblePlan{}, false
	}
	if _, ok := b.center.AwaitTrigger(id, now); !ok {
		return bubblePlan{}, false
	}
	return b.advance(now), true
}

// startTrigger 在宿主确认展示时启动当前关键词动作的五秒计时。
func (b *bubbleState) startTrigger(id string, now time.Time) (bubblePlan, bool) {
	if b.center == nil {
		return bubblePlan{}, false
	}
	if _, ok := b.center.StartTrigger(id, now); !ok {
		return bubblePlan{}, false
	}
	return b.advance(now), true
}

// cancelUnstartedTrigger 只跳过指定的未展示动作，并让下一条提醒继续排队展示。
func (b *bubbleState) cancelUnstartedTrigger(id string, now time.Time) (bubblePlan, bool) {
	if b.center == nil || !b.center.CancelUnstartedTrigger(id) {
		return bubblePlan{}, false
	}
	return b.advance(now), true
}

// advance 仅在通知身份改变时要求重绘，定时器始终使用中心提供的绝对时间。
func (b *bubbleState) advance(now time.Time) bubblePlan {
	if b.center == nil {
		return bubblePlan{version: b.version}
	}
	entry, ok := b.center.Current(now)
	if !ok {
		entry = notification.Entry{}
	}
	changed := entry.ID != b.entryID
	if changed {
		b.entryID = entry.ID
		b.suppressed = false
		b.version++
	}
	deadline, timed := b.center.NextDeadline()
	if b.suppressed {
		entry = notification.Entry{}
	}
	return bubblePlan{version: b.version, changed: changed, entry: entry, deadline: deadline, timed: timed}
}

// dismiss 使已入 UI 队列的旧显示失效，同时保留下一条提醒的推进时钟。
func (b *bubbleState) dismiss(now time.Time) bubblePlan {
	plan := b.advance(now)
	b.suppressed = true
	b.version++
	plan.version = b.version
	plan.changed = true
	plan.entry = notification.Entry{}
	return plan
}
