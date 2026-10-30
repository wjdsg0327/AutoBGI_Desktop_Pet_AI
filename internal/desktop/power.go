package desktop

import (
	"sync"
	"time"
)

// powerController 只在锁内记录最后意图，消费回调可重入或触发服务广播而不会重复进入转换。
type powerController struct {
	mu       sync.Mutex
	known    bool
	sleeping bool
	revision uint64
	applying bool
}

// set 让同一消费者连续处理最新意图，阻塞期间收到的唤醒不会被旧暂停动作反向覆盖。
func (c *powerController) set(sleeping bool, apply func(bool)) {
	c.mu.Lock()
	if c.known && c.sleeping == sleeping {
		c.mu.Unlock()
		return
	}
	c.known, c.sleeping = true, sleeping
	c.revision++
	if c.applying {
		c.mu.Unlock()
		return
	}
	c.applying = true
	c.mu.Unlock()
	completed := false
	defer func() {
		// 原生异常交给宿主处理，但必须释放消费标记，后续电源消息仍可恢复状态。
		if !completed {
			c.mu.Lock()
			c.applying = false
			c.known = false
			c.mu.Unlock()
		}
	}()
	for {
		c.mu.Lock()
		current, revision := c.sleeping, c.revision
		c.mu.Unlock()
		apply(current)
		c.mu.Lock()
		if revision == c.revision || current == c.sleeping {
			c.applying = false
			completed = true
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
	}
}

// setPowerIntent 接受宿主电源消息；Windows 调用源位于主线程原始消息处理，不经过无序事件协程。
func (d *DesktopService) setPowerIntent(sleeping bool) {
	if !d.stopped() {
		// 先锁存原始意图并撤销手势，消费器合并休眠与恢复时也不能吞掉中间中断。
		d.mu.Lock()
		if sleeping || d.powerSleepingIntent != sleeping {
			d.drag.invalidate()
		}
		d.powerSleepingIntent = sleeping
		d.mu.Unlock()
		d.power.set(sleeping, d.setSleeping)
	}
}

// setSleeping 由唯一电源消费者调用，窗口状态与采集暂停按同一顺序更新，服务和原生回调均在锁外。
func (d *DesktopService) setSleeping(sleeping bool) {
	if d.stopped() {
		return
	}
	var plan bubblePlan
	// 直接消费入口也保留中断保护，系统重复恢复不能让旧手势重新有效。
	d.invalidatePetDrag()
	if sleeping {
		// 先在业务层禁止投递并提交勿扰消费，再更新窗口；同步广播不得持有宿主锁。
		d.service.SetNotificationDeliveryBlocked(true)
		d.mu.Lock()
		d.sleeping = true
		plan = d.notices.update(d.notificationStateLocked(), true, time.Now())
		d.prepareBubbleLocked(plan)
		d.mu.Unlock()
	} else {
		// 先消费休眠前未进入 UI 队列的历史，再解除阻止，唤醒不会集中补弹。
		state := d.service.GetState()
		d.mu.Lock()
		state = d.visibleNotificationStateLocked(state)
		blocked := d.notices.update(state, true, time.Now())
		d.sleeping = false
		plan = d.notices.update(state, false, time.Now())
		plan.changed = plan.changed || blocked.changed
		d.prepareBubbleLocked(plan)
		d.mu.Unlock()
	}
	d.service.Pause(sleeping)
	if sleeping {
		d.HideTodoWindow()
	}
	if !sleeping {
		// 唤醒前的实时队列已按禁播消费，解除运行时门禁也不会把历史重新送进气泡。
		d.service.SetNotificationDeliveryBlocked(false)
	}
	d.dispatchBubble(plan)
	if !sleeping {
		d.correctPosition()
	}
	d.emitPresentation()
}
