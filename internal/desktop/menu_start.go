package desktop

import (
	"fmt"
	"time"

	"autobgi-pet/internal/notification"
	"autobgi-pet/internal/service"
)

// showMenuStartBubble 在桌面锁内读取实时来源并排队，两种启动都不能将旧结果归入新连接。
func (d *DesktopService) showMenuStartBubble(text, kind, profileID, activeProfileID string, generation uint64) {
	d.mu.Lock()
	state := d.service.GetState()
	plan, accepted := d.offerMenuStartBubbleLocked(state, text, kind, profileID, activeProfileID, generation, time.Now())
	d.mu.Unlock()
	if accepted {
		d.dispatchBubble(plan)
	}
}

// offerMenuStartBubbleLocked 将身份检查、作用域同步和入队放在同一临界区，调用者必须持有桌面锁。
func (d *DesktopService) offerMenuStartBubbleLocked(state service.AppState, text, kind, profileID, activeProfileID string, generation uint64, now time.Time) (bubblePlan, bool) {
	if d.stopped() || !oneLongMenuIdentityMatches(state, profileID, activeProfileID, generation) {
		return bubblePlan{}, false
	}
	scope := notificationScope(state)
	// 通知中心拒绝回滚代次时必须停止入队，不能把旧文本放进保留下来的新队列。
	if scope.Generation < d.notices.scope.Generation {
		return bubblePlan{}, false
	}
	if d.notices.center == nil || d.notices.scope != scope {
		_ = d.notices.update(d.visibleNotificationStateLocked(state), d.sleeping, now)
	}
	if d.notices.scope != scope {
		return bubblePlan{}, false
	}
	d.manualNoticeSequence++
	plan := d.notices.offer(notification.Entry{ID: fmt.Sprintf("manual:%d", d.manualNoticeSequence), Kind: kind, Message: text, CreatedAt: now.Format(time.RFC3339Nano)}, now)
	d.prepareBubbleLocked(plan)
	return plan, true
}
