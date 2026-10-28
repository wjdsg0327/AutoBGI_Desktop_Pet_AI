//go:build !windows

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// HostWindowsOptions 在非 Windows 平台不安装消息拦截，不声称具备 Windows 原始电源事件顺序保证。
//
//wails:ignore
func (d *DesktopService) HostWindowsOptions() application.WindowsOptions {
	return application.WindowsOptions{}
}

// bindPowerEvents 保留其他平台的通用事件兼容入口，串行更新本地状态但不推断框架原始事件先后。
func (d *DesktopService) bindPowerEvents(app *application.App) {
	app.Event.OnApplicationEvent(events.Common.SystemWillSleep, func(*application.ApplicationEvent) { d.setPowerIntent(true) })
	app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) { d.setPowerIntent(false) })
}
