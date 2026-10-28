//go:build !windows

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// bindNetworkEvents 在其他平台把通用事件当作重新读取本机状态的信号，不猜测平台 payload。
func (d *DesktopService) bindNetworkEvents(app *application.App) {
	changes := make(chan struct{}, 1)
	app.Event.OnApplicationEvent(events.Common.NetworkChanged, func(*application.ApplicationEvent) {
		select {
		case changes <- struct{}{}:
		default:
		}
	})
	d.startNetworkMonitor(changes, func() {}, nil)
}
