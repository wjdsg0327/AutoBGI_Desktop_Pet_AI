//go:build windows

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// 原始参数对应 WM_POWERBROADCAST 与系统挂起、自动恢复和用户恢复消息，不改变系统电源状态。
const (
	powerBroadcastMessage uint32  = 0x0218
	powerSuspendParameter uintptr = 0x0004
	powerAutomaticResume  uintptr = 0x0012
	powerUserResume       uintptr = 0x0007
)

// interceptPowerMessages 同步接收原始电源消息，保留框架后续处理，不调用任何真实休眠或唤醒接口。
func interceptPowerMessages(handle func(bool)) func(uintptr, uint32, uintptr, uintptr) (uintptr, bool) {
	return func(_ uintptr, message uint32, parameter, _ uintptr) (uintptr, bool) {
		if message == powerBroadcastMessage {
			switch parameter {
			case powerSuspendParameter:
				handle(true)
			case powerAutomaticResume, powerUserResume:
				handle(false)
			}
		}
		return 0, false
	}
}

// HostWindowsOptions 仅供主程序接线，原始消息按 Windows 主线程顺序锁存，不依赖 Wails 无序事件监听。
//
//wails:ignore
func (d *DesktopService) HostWindowsOptions() application.WindowsOptions {
	// 鼠标和程序触发的激活均只拦截桌宠类窗口，其他消息仍按原主线程顺序处理。
	return application.WindowsOptions{WndProcInterceptor: d.activationGuard(interceptWindowActivation(
		interceptDragEnvironmentMessages(interceptPowerMessages(d.setPowerIntent), func() { d.invalidatePetDrag() }),
		hostWindowExStyle,
	))}
}

// bindPowerEvents 在 Windows 上不再订阅通用睡眠事件，避免框架异步转发重复或颠倒原始消息。
func (d *DesktopService) bindPowerEvents(*application.App) {}
