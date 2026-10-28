//go:build windows

package desktop

import (
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// bindNetworkEvents 不依赖 Wails 移动端专用的 NetworkChanged payload，直接订阅 Windows 接口和地址变化。
func (d *DesktopService) bindNetworkEvents(*application.App) {
	changes := make(chan struct{}, 1)
	// 系统回调只发出有界信号，不读取瞬时 Row 指针，也不在回调中等待或注销自身。
	callback := syscall.NewCallback(func(_, _, _ uintptr) uintptr {
		select {
		case <-d.done:
			return 0
		default:
		}
		select {
		case changes <- struct{}{}:
		default:
		}
		return 0
	})
	var interfaceHandle, addressHandle windows.Handle
	interfaceErr := windows.NotifyIpInterfaceChange(windows.AF_UNSPEC, callback, nil, false, &interfaceHandle)
	addressErr := windows.NotifyUnicastIpAddressChange(windows.AF_UNSPEC, callback, nil, false, &addressHandle)
	release := func() {
		// 注销发生在独立工作协程，遵守 Windows 不得在被注销回调内等待自己的约束。
		if interfaceErr == nil {
			_ = windows.CancelMibChangeNotify2(interfaceHandle)
		}
		if addressErr == nil {
			_ = windows.CancelMibChangeNotify2(addressHandle)
		}
	}
	var registrationErr error
	if interfaceErr != nil || addressErr != nil {
		registrationErr = networkRegistrationError()
	}
	d.startNetworkMonitor(changes, release, registrationErr)
}
