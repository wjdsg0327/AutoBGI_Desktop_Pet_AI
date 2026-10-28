package desktop

import "sync"

// recoveryShortcutView 仅发布白名单组合与固定提示，不返回原生错误、路径或系统按键上下文。
type recoveryShortcutView struct {
	Requested  string `json:"requested"`
	Registered string `json:"registered"`
	Status     string `json:"status"`
	Retryable  bool   `json:"retryable"`
	Warning    string `json:"warning"`
}

// recoveryShortcutController 将原生注册回调与状态分离，注册串行执行，快照可并发读取。
type recoveryShortcutController struct {
	operation sync.Mutex
	mu        sync.Mutex
	view      recoveryShortcutView
}

// supportedRecoveryShortcut 与业务偏好的固定选项一致，不让重试入口接受任意按键串。
func supportedRecoveryShortcut(value string) bool {
	switch value {
	case "Ctrl+Shift+P", "Ctrl+Alt+P", "Ctrl+Shift+Y":
		return true
	default:
		return false
	}
}

// snapshot 按当前已保存期望解释实际绑定；迟到的旧尝试不能显示为新偏好已生效。
func (c *recoveryShortcutController) snapshot(requested string, queued bool) recoveryShortcutView {
	c.mu.Lock()
	view := c.view
	c.mu.Unlock()
	if requested == "" {
		requested = "Ctrl+Shift+P"
	}
	if !supportedRecoveryShortcut(requested) {
		view.Requested = ""
		view.Status, view.Retryable = "unavailable", false
		view.Warning = "恢复快捷键设置无效，请从托盘打开设置并选择支持的组合。"
		return view
	}
	if queued || view.Requested != requested || view.Status == "" {
		view.Status, view.Retryable, view.Warning = "pending", false, ""
	}
	view.Requested = requested
	return view
}

// apply 先取得新绑定再释放旧绑定；注册失败只更新固定告警，旧恢复入口继续有效。
func (c *recoveryShortcutController) apply(requested string, register, unregister func(string) error) {
	c.operation.Lock()
	defer c.operation.Unlock()
	if requested == "" {
		requested = "Ctrl+Shift+P"
	}
	c.mu.Lock()
	old := c.view.Registered
	if !supportedRecoveryShortcut(requested) {
		c.view = recoveryShortcutView{
			Registered: old, Status: "unavailable",
			Warning: "恢复快捷键设置无效，请从托盘打开设置并选择支持的组合。",
		}
		c.mu.Unlock()
		return
	}
	if old == requested {
		// 改回仍有效的旧组合也算恢复成功，必须清除先前尝试留下的冲突提示。
		c.view = recoveryShortcutView{Requested: requested, Registered: old, Status: "registered"}
		c.mu.Unlock()
		return
	}
	c.view = recoveryShortcutView{Requested: requested, Registered: old, Status: "pending"}
	c.mu.Unlock()
	if register == nil {
		c.mu.Lock()
		c.view = recoveryShortcutView{
			Requested: requested, Registered: old, Status: "unavailable", Retryable: true,
			Warning: "恢复快捷键暂时不可注册，请从系统托盘恢复交互，或稍后重试。",
		}
		c.mu.Unlock()
		return
	}
	if err := register(requested); err != nil {
		warning := "恢复快捷键 " + requested + " 注册失败，可关闭占用该组合的程序后重试；系统托盘仍可恢复交互。"
		if old != "" {
			warning += " 当前仍可使用 " + old + "。"
		}
		c.mu.Lock()
		c.view = recoveryShortcutView{Requested: requested, Registered: old, Status: "conflict", Retryable: true, Warning: warning}
		c.mu.Unlock()
		return
	}
	warning := ""
	if old != "" && unregister != nil {
		// 新绑定已经有效；旧绑定释放失败只提示清理边界，不能撤销新的恢复入口。
		if unregister(old) != nil {
			warning = "新的恢复快捷键已生效，但旧组合释放未确认；系统托盘仍可恢复交互，退出桌宠后会清理旧绑定。"
		}
	}
	c.mu.Lock()
	c.view = recoveryShortcutView{Requested: requested, Registered: requested, Status: "registered", Warning: warning}
	c.mu.Unlock()
}
