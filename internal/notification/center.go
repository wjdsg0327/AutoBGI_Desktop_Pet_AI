// Package notification 提供只管理内存状态的通知队列，时间和窗口展示均由调用方控制。
package notification

import (
	"fmt"
	"strings"
	"time"
)

const (
	queueLimit   = 20
	seenLimit    = 4096
	displayTime  = 5 * time.Second
	pickupTime   = 1500 * time.Millisecond
	confirmTime  = 30 * time.Second
	historyLimit = 5 * time.Minute
	futureLimit  = time.Minute
)

// Scope 将通知绑定到连接代次和服务实例；Generation 应随连接上下文单调递增。
type Scope struct {
	ProfileID  string
	Generation uint64
	InstanceID string
}

// Entry 直接接受业务通知的时间字符串，不修改原始事实内容或根据日志生成任务终态。
type Entry struct {
	ID        string
	Kind      string
	Message   string
	CreatedAt string
}

// Policy 区分勿扰计数与禁止补播；隐藏、休眠和关闭气泡的优先级高于勿扰。
type Policy struct {
	Hidden         bool
	Sleeping       bool
	BubbleDisabled bool
	DoNotDisturb   bool
	QuietManaged   bool // 业务层已持久管理勿扰摘要时，展示队列只执行抑制，避免再次计数或补弹。
}

// queued 保存聚合提醒所代表的数量，防止队列限流后丢失勿扰摘要中的计数。
type queued struct {
	entry     Entry
	createdAt time.Time
	retryAt   time.Time
	count     int
	errors    int
	aggregate bool
}

// seenEntry 保留本地消费时间，在最近四千余条内精确去重，不用时间水位误吞相同时间戳的新提醒。
type seenEntry struct {
	id     string
	seenAt time.Time
}

// Center 不创建定时器或 goroutine，所有方法由桌面层在同一把锁内串行调用。
type Center struct {
	scope         Scope
	hasScope      bool
	policy        Policy
	pending       []queued
	active        *queued
	deadline      time.Time
	confirmUntil  time.Time
	quietCount    int
	seen          map[string]bool
	seenOrder     []seenEntry
	summaryNumber uint64
	fainted       bool
}

// New 创建独立通知中心；零值也可直接使用。
func New() *Center { return &Center{seen: make(map[string]bool)} }

// Update 摄入整批未见提醒；上下文切换只清展示状态，已见 ID 跨上下文保留以防初连历史重播。
func (c *Center) Update(scope Scope, entries []Entry, policy Policy, now time.Time) {
	if c.hasScope && scope.Generation < c.scope.Generation {
		return
	}
	c.pruneSeen(now)
	if !c.hasScope || c.scope != scope {
		c.scope, c.hasScope = scope, true
		c.clearPresentation()
		c.policy = Policy{}
	}
	wasQuiet := c.policy.DoNotDisturb
	c.policy = policy
	if policy.QuietManaged {
		c.quietCount = 0
	}
	if c.blocked() {
		c.suspendActions()
	} else if policy.DoNotDisturb {
		// 已展示的提醒不重复计数；尚未展示的队列转换成一个待恢复摘要的数量。
		if !policy.QuietManaged {
			for _, item := range c.pending {
				if item.entry.Kind != "pickup" && item.entry.Kind != "trigger" && now.Sub(item.createdAt) <= historyLimit {
					c.quietCount += item.count
				}
			}
		}
		c.suspendActions()
	} else if wasQuiet && !policy.QuietManaged && c.quietCount > 0 {
		count := c.quietCount
		c.quietCount = 0
		c.enqueue(c.summary(count, 0, "勿扰已结束，期间有 %d 条状态提醒。", now))
	}
	for _, entry := range entries {
		item, fresh := c.consume(entry, now)
		if !fresh || c.blocked() {
			continue
		}
		if policy.DoNotDisturb {
			if !policy.QuietManaged {
				c.quietCount++
			}
			continue
		}
		c.enqueue(item)
	}
}

// suspendActions 在窗口不可展示期间保留动作队列，恢复后逐项重播。
func (c *Center) suspendActions() {
	kept := make([]queued, 0)
	if c.active != nil && (c.active.entry.Kind == "pickup" || c.active.entry.Kind == "trigger") {
		kept = append(kept, *c.active)
	}
	for _, item := range c.pending {
		if item.entry.Kind == "pickup" || item.entry.Kind == "trigger" {
			kept = append(kept, item)
		}
	}
	c.pending, c.active, c.deadline, c.confirmUntil = kept, nil, time.Time{}, time.Time{}
}

// Current 选取当前提醒；关键词等待实际展示后开始计时，普通提醒仍从选取时计时。
func (c *Center) Current(now time.Time) (Entry, bool) {
	if !c.hasScope || c.blocked() || c.policy.DoNotDisturb || c.fainted {
		return Entry{}, false
	}
	if c.active != nil && ((!c.deadline.IsZero() && !now.Before(c.deadline)) ||
		(!c.confirmUntil.IsZero() && !now.Before(c.confirmUntil))) {
		// 素材或窗口迟迟未就绪时，拾取保留在同类队首，短暂等待后再尝试。
		if c.active.entry.Kind == "pickup" && !c.confirmUntil.IsZero() && !now.Before(c.confirmUntil) {
			item := *c.active
			item.retryAt = now.Add(5 * time.Second)
			c.pending = append([]queued{item}, c.pending...)
		}
		c.active, c.deadline, c.confirmUntil = nil, time.Time{}, time.Time{}
	}
	// 排队时间较长的历史提醒直接丢弃，不在恢复或长队列后集中补弹。
	kept := c.pending[:0]
	for _, item := range c.pending {
		if item.entry.Kind == "pickup" || item.entry.Kind == "trigger" || now.Sub(item.createdAt) <= historyLimit {
			kept = append(kept, item)
		}
	}
	c.pending = kept
	index := c.bestIndex(now)
	if index < 0 {
		if c.active == nil {
			return Entry{}, false
		}
		return c.active.entry, true
	}
	if c.active == nil || c.pending[index].entry.Kind == "error" && c.active.entry.Kind != "error" && c.active.entry.Kind != "trigger" && c.active.entry.Kind != "pickup" {
		// 被抢占的提醒已经尝试展示，不重新排队，以免同一 ID 被再次弹出。
		item := c.pending[index]
		c.pending = append(c.pending[:index], c.pending[index+1:]...)
		c.activate(item, now)
	}
	return c.active.entry, true
}

// NextDeadline 返回展示截止时间或前端确认期限，未发布的关键词不启动定时器。
func (c *Center) NextDeadline() (time.Time, bool) {
	if c.blocked() || c.policy.DoNotDisturb || c.fainted {
		return time.Time{}, false
	}
	if c.active == nil {
		// 所有拾取均在重试等待时也需要唤醒时钟，避免队列永久停住。
		var retry time.Time
		for _, item := range c.pending {
			if !item.retryAt.IsZero() && (retry.IsZero() || item.retryAt.Before(retry)) {
				retry = item.retryAt
			}
		}
		return retry, !retry.IsZero()
	}
	if !c.deadline.IsZero() {
		return c.deadline, true
	}
	return c.confirmUntil, !c.confirmUntil.IsZero()
}

// AwaitTrigger 在动作通知前设置确认期限，重复发布不能延长等待时间。
func (c *Center) AwaitTrigger(id string, now time.Time) (time.Time, bool) {
	if !c.hasScope || c.blocked() || c.policy.DoNotDisturb || c.active == nil ||
		(c.active.entry.Kind != "trigger" && c.active.entry.Kind != "pickup") || c.fainted || c.active.entry.ID != id ||
		!c.deadline.IsZero() || !c.confirmUntil.IsZero() {
		return time.Time{}, false
	}
	c.confirmUntil = now.Add(confirmTime)
	return c.confirmUntil, true
}

// StartTrigger 仅接受期限内的当前动作确认，重复和过时的请求不改变期限。
func (c *Center) StartTrigger(id string, now time.Time) (time.Time, bool) {
	if !c.hasScope || c.blocked() || c.policy.DoNotDisturb || c.active == nil ||
		(c.active.entry.Kind != "trigger" && c.active.entry.Kind != "pickup") || c.fainted || c.active.entry.ID != id || !c.deadline.IsZero() ||
		c.confirmUntil.IsZero() || !now.Before(c.confirmUntil) {
		return time.Time{}, false
	}
	duration := displayTime
	if c.active.entry.Kind == "pickup" {
		duration = pickupTime
	}
	c.deadline = now.Add(duration)
	c.confirmUntil = time.Time{}
	return c.deadline, true
}

// CancelUnstartedTrigger 只取消尚未展示且身份匹配的关键词动作，避免旧回调影响新的提醒。
func (c *Center) CancelUnstartedTrigger(id string) bool {
	if c.active == nil || (c.active.entry.Kind != "trigger" && c.active.entry.Kind != "pickup") ||
		c.active.entry.ID != id || !c.deadline.IsZero() {
		return false
	}
	c.active, c.confirmUntil = nil, time.Time{}
	return true
}

// activate 让普通提醒立即计时，关键词动作则等待宿主确认展示。
func (c *Center) activate(item queued, now time.Time) {
	c.active = &item
	c.deadline = time.Time{}
	c.confirmUntil = time.Time{}
	if item.entry.Kind != "trigger" && item.entry.Kind != "pickup" {
		c.deadline = now.Add(displayTime)
	}
}

// OfferManual 接受本地状态或明确系统错误；普通点击不抢占命令和错误，系统错误可抢占低优先级。
func (c *Center) OfferManual(scope Scope, entry Entry, now time.Time) bool {
	if !c.hasScope || c.scope != scope {
		return false
	}
	c.pruneSeen(now)
	// 状态类型交给界面识别排版，关键词则保留独立类型以按顺序播放。
	if entry.Kind != "error" && entry.Kind != "trigger" && entry.Kind != "status" {
		entry.Kind = "manual"
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = now.Format(time.RFC3339Nano)
	}
	item, fresh := c.consume(entry, now)
	if !fresh || c.blocked() || c.policy.DoNotDisturb {
		return false
	}
	_, _ = c.Current(now)
	if !c.fainted && (c.active == nil || priority(c.active.entry.Kind) < priority(item.entry.Kind) && c.active.entry.Kind != "pickup" && c.active.entry.Kind != "trigger") {
		c.activate(item, now)
	} else {
		// 本地动作不能变成摘要；队列满时明确拒绝，由调用方显示未播放数量。
		if c.ordinaryPending() >= queueLimit {
			return false
		}
		c.enqueue(item)
	}
	return true
}

// OfferPickup 保留每一条拾取，独立于普通通知的容量和历史时限。
func (c *Center) OfferPickup(scope Scope, entry Entry, now time.Time) bool {
	if !c.hasScope || c.scope != scope || entry.ID == "" || strings.TrimSpace(entry.Message) == "" {
		return false
	}
	entry.Kind = "pickup"
	if entry.CreatedAt == "" {
		entry.CreatedAt = now.Format(time.RFC3339Nano)
	}
	item := queued{entry: entry, createdAt: now, count: 1}
	if c.active == nil && !c.fainted && !c.blocked() && !c.policy.DoNotDisturb && len(c.pending) == 0 {
		c.activate(item, now)
	} else {
		c.pending = append(c.pending, item)
	}
	return true
}

// SetFainted 在饿晕时将当前动作放回队首，恢复后从首帧重新完整播放。
func (c *Center) SetFainted(fainted bool) {
	if c.fainted == fainted {
		return
	}
	c.fainted = fainted
	if fainted && c.active != nil && (c.active.entry.Kind == "trigger" || c.active.entry.Kind == "pickup") {
		c.pending = append([]queued{*c.active}, c.pending...)
		c.active, c.deadline, c.confirmUntil = nil, time.Time{}, time.Time{}
	}
}

// ClearPickups 在关闭拾取通知时移除当前及所有待播蛋糕动作。
func (c *Center) ClearPickups() {
	kept := c.pending[:0]
	for _, item := range c.pending {
		if item.entry.Kind != "pickup" {
			kept = append(kept, item)
		}
	}
	c.pending = kept
	if c.active != nil && c.active.entry.Kind == "pickup" {
		c.active, c.deadline, c.confirmUntil = nil, time.Time{}, time.Time{}
	}
}

// HasAction 判断动作是否仍处于活动或等待状态，供宿主保留重试所需资料。
func (c *Center) HasAction(id string) bool {
	if c.active != nil && c.active.entry.ID == id {
		return true
	}
	for _, item := range c.pending {
		if item.entry.ID == id {
			return true
		}
	}
	return false
}

// TriggerQueueFull 用于向日志界面报告未能进入队列的动作，而不是静默合并。
func (c *Center) TriggerQueueFull() bool { return c.ordinaryPending() >= queueLimit }

// ordinaryPending 只对普通提醒限流，拾取动作单独保留。
func (c *Center) ordinaryPending() int {
	count := 0
	for _, item := range c.pending {
		if item.entry.Kind != "pickup" {
			count++
		}
	}
	return count
}

// mergeablePending 只统计允许合并的普通通知，已接收的动作不能作为摘要牺牲项。
func (c *Center) mergeablePending() int {
	count := 0
	for _, item := range c.pending {
		if item.entry.Kind != "pickup" && item.entry.Kind != "trigger" {
			count++
		}
	}
	return count
}

// Pending 返回尚未展示的队列长度，活动气泡不计入容量为二十的待展示队列。
func (c *Center) Pending() int { return len(c.pending) }

// QuietCount 返回勿扰期间尚未汇总的提醒数量，重复快照不会增加该计数。
func (c *Center) QuietCount() int { return c.quietCount }

// blocked 的禁止补播策略优先于勿扰恢复，确保隐藏或关闭提醒后不会积累待弹气泡。
func (c *Center) blocked() bool {
	return c.policy.Hidden || c.policy.Sleeping || c.policy.BubbleDisabled
}

// clearPresentation 只清理当前上下文的展示和计数，跨上下文 ID 去重记录仍然保留。
func (c *Center) clearPresentation() {
	c.pending, c.active, c.deadline, c.confirmUntil, c.quietCount = nil, nil, time.Time{}, time.Time{}, 0
}

// consume 先记已见再判断新鲜度，过期、无效和禁止展示的历史都不会在后续状态中补播。
func (c *Center) consume(entry Entry, now time.Time) (queued, bool) {
	if entry.ID == "" || c.seen[entry.ID] {
		return queued{}, false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
	c.remember(entry.ID, now)
	// 空台词的关键词规则仍需播放动作；其他空通知继续忽略。
	if err != nil || strings.TrimSpace(entry.Message) == "" && entry.Kind != "trigger" || now.Sub(createdAt) > historyLimit ||
		createdAt.Sub(now) > futureLimit {
		return queued{}, false
	}
	if entry.Kind == "" {
		entry.Kind = "info"
	}
	item := queued{entry: entry, createdAt: createdAt, count: 1}
	if entry.Kind == "error" {
		item.errors = 1
	}
	return item, true
}

// remember 限制已见 ID 的内存容量；外部持久事件日志负责更早历史的去重，此处不按时间误杀新 ID。
func (c *Center) remember(id string, now time.Time) {
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	c.seen[id] = true
	c.seenOrder = append(c.seenOrder, seenEntry{id: id, seenAt: now})
	if len(c.seenOrder) > seenLimit {
		delete(c.seen, c.seenOrder[0].id)
		c.seenOrder = c.seenOrder[1:]
	}
}

// pruneSeen 只删除已超过最大可展示时间窗口的记录；对应原始历史仍会被新鲜度检查拒绝。
func (c *Center) pruneSeen(now time.Time) {
	kept := c.seenOrder[:0]
	for _, item := range c.seenOrder {
		if now.Sub(item.seenAt) > historyLimit+futureLimit {
			delete(c.seen, item.id)
			continue
		}
		kept = append(kept, item)
	}
	c.seenOrder = kept
}

// enqueue 合并同一上下文的待展示进度或手动状态，溢出时保留高优先级并生成一个计数摘要。
func (c *Center) enqueue(item queued) {
	// 状态和手动通知共用合并类别，保留最近一次手动交互的原有行为。
	mergeKind := func(kind string) string {
		if kind == "status" {
			return "manual"
		}
		return kind
	}
	kind := mergeKind(item.entry.Kind)
	if kind == "progress" || kind == "manual" {
		for index := len(c.pending) - 1; index >= 0; index-- {
			if mergeKind(c.pending[index].entry.Kind) == kind {
				c.pending = append(c.pending[:index], c.pending[index+1:]...)
			}
		}
	}
	c.pending = append(c.pending, item)
	if c.mergeablePending() <= queueLimit {
		return
	}
	// 首次溢出需要让出一个摘要槽；后续溢出继续合并进已有摘要，不不断添加摘要气泡。
	aggregateIndex := -1
	for index, candidate := range c.pending {
		if candidate.aggregate {
			aggregateIndex = index
			break
		}
	}
	var aggregate queued
	if aggregateIndex >= 0 {
		aggregate = c.pending[aggregateIndex]
		c.pending = append(c.pending[:aggregateIndex], c.pending[aggregateIndex+1:]...)
	}
	for c.mergeablePending() >= queueLimit {
		victim := c.lowestIndex()
		aggregate.count += c.pending[victim].count
		aggregate.errors += c.pending[victim].errors
		if c.pending[victim].createdAt.After(aggregate.createdAt) {
			aggregate.createdAt = c.pending[victim].createdAt
		}
		c.pending = append(c.pending[:victim], c.pending[victim+1:]...)
	}
	if aggregate.entry.ID == "" {
		aggregate = c.summary(aggregate.count, aggregate.errors, "另有 %d 条状态提醒已合并。", aggregate.createdAt)
	} else {
		aggregate.entry.Message = fmt.Sprintf("另有 %d 条状态提醒已合并。", aggregate.count)
		aggregate.entry.CreatedAt = aggregate.createdAt.Format(time.RFC3339Nano)
	}
	if aggregate.errors > 0 {
		aggregate.entry.Kind = "error"
		aggregate.entry.Message = fmt.Sprintf("%d 条提醒已合并，其中 %d 条为错误提醒。", aggregate.count, aggregate.errors)
	}
	c.pending = append(c.pending, aggregate)
}

// summary 的标识在本通知中心内单调生成，不冒充服务端事件或任务标识。
func (c *Center) summary(count, errorCount int, message string, now time.Time) queued {
	c.summaryNumber++
	return queued{
		entry: Entry{ID: fmt.Sprintf("notification-summary:%d", c.summaryNumber), Kind: "summary",
			Message: fmt.Sprintf(message, count), CreatedAt: now.Format(time.RFC3339Nano)},
		createdAt: now, count: count, errors: errorCount, aggregate: true,
	}
}

// bestIndex 在相同优先级内保持入队顺序，避免后到的普通消息覆盖尚未展示的结果。
func (c *Center) bestIndex(now time.Time) int {
	best := -1
	pickupSeen := false
	for index, item := range c.pending {
		// 最早的拾取正在等待重试时，后续拾取不能越过它。
		if item.entry.Kind == "pickup" {
			if pickupSeen {
				continue
			}
			pickupSeen = true
		}
		if !item.retryAt.IsZero() && now.Before(item.retryAt) {
			continue
		}
		if best < 0 || priority(item.entry.Kind) > priority(c.pending[best].entry.Kind) {
			best = index
		}
	}
	return best
}

// lowestIndex 从低优先级的最新项开始合并，较早的错误和命令结果优先保留独立展示。
func (c *Center) lowestIndex() int {
	lowest := -1
	for index, item := range c.pending {
		if item.entry.Kind != "pickup" && item.entry.Kind != "trigger" && (lowest < 0 || priority(item.entry.Kind) <= priority(c.pending[lowest].entry.Kind)) {
			lowest = index
		}
	}
	return lowest
}

// priority 仅对已知事实种类排序，手动交互低于错误和命令结果，其余信息按普通提醒处理。
func priority(kind string) int {
	switch kind {
	case "error":
		return 5
	case "command":
		return 4
	case "manual", "status", "summary", "trigger", "pickup":
		return 3
	case "connection":
		return 2
	case "progress":
		return 1
	default:
		return 0
	}
}
