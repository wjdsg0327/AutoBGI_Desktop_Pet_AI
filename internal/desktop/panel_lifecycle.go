package desktop

import "autobgi-pet/internal/profile"

// panelIntent 只描述本程序面板的展示意图，不持有原生句柄或业务连接。
type panelIntent struct {
	visible  bool
	focus    bool
	revision uint64
}

// panelLifecycle 在宿主锁内保存意图；原生调用期间仍允许嵌套消息更新下一次意图。
type panelLifecycle struct {
	started         bool
	requested       bool
	visible         bool
	focus           bool
	revision        uint64
	pending         bool
	queued          bool
	applying        bool
	trayFallbackSet bool
}

// request 合并连续打开请求；隐藏立即取消尚未消费的聚焦，且本身不会创建面板。
func (p *panelLifecycle) request(visible, focus bool) bool {
	p.requested = true
	p.visible = visible
	p.focus = visible && (p.focus || focus)
	p.revision++
	p.pending = true
	return p.queue()
}

// start 只在没有更早的显式请求时补上首次设置，不会覆盖启动期间的关闭请求。
func (p *panelLifecycle) start(needsSetup bool) bool {
	if p.started {
		return false
	}
	p.started = true
	if needsSetup && !p.requested {
		return p.request(true, false)
	}
	return p.queue()
}

// trayFailed 仅第一次托盘创建失败请求可见兜底，重复告警不能反复打开已关闭的面板。
func (p *panelLifecycle) trayFailed() bool {
	if p.trayFallbackSet {
		return false
	}
	p.trayFallbackSet = true
	return p.request(true, false)
}

// queue 保证 UI 队列至多保留一个面板消费者，创建期间的重入交给当前消费者收尾。
func (p *panelLifecycle) queue() bool {
	if !p.started || !p.pending || p.queued || p.applying {
		return false
	}
	p.queued = true
	return true
}

// begin 必须由 UI 消费者调用；嵌套消息不能再次进入原生面板生命周期。
func (p *panelLifecycle) begin() bool {
	p.queued = false
	if !p.started || !p.pending || p.applying {
		return false
	}
	p.applying = true
	return true
}

// take 在窗口创建完成后读取最新意图，避免慢初始化重新显示已经取消的打开请求。
func (p *panelLifecycle) take() panelIntent {
	intent := panelIntent{visible: p.visible, focus: p.focus, revision: p.revision}
	p.pending = false
	p.focus = false
	return intent
}

// mayFocus 拒绝在原生 Show 重入后继续执行过时的聚焦请求。
func (p *panelLifecycle) mayFocus(intent panelIntent) bool {
	return intent.focus && p.visible && p.revision == intent.revision
}

// finish 在原生调用全部返回后安排尚未处理的请求，不在持锁期间调用窗口方法。
func (p *panelLifecycle) finish() bool {
	p.applying = false
	return p.queue()
}

// needsInitialSetup 只检查本地脱敏资料；离线、重连和认证失败不会触发自动弹窗。
func needsInitialSetup(profiles []profile.ProfileView, activeID string) bool {
	for _, p := range profiles {
		if p.ID == activeID && p.HasCredential {
			return false
		}
	}
	return true
}
