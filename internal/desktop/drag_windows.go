//go:build windows

package desktop

import "github.com/wailsapp/wails/v3/pkg/w32"

// interceptDragEnvironmentMessages 先锁存显示、工作区和 DPI 中断，再保留宿主原消息处理。
func interceptDragEnvironmentMessages(next func(uintptr, uint32, uintptr, uintptr) (uintptr, bool),
	invalidate func(),
) func(uintptr, uint32, uintptr, uintptr) (uintptr, bool) {
	return func(hwnd uintptr, message uint32, parameter, data uintptr) (uintptr, bool) {
		// 只匹配会改变坐标环境的系统设置，主题或其他无关设置不取消正常拖动。
		if message == w32.WM_DISPLAYCHANGE || message == w32.WM_DPICHANGED ||
			message == w32.WM_SETTINGCHANGE && parameter == w32.SPI_SETWORKAREA {
			if invalidate != nil {
				invalidate()
			}
		}
		if next != nil {
			return next(hwnd, message, parameter, data)
		}
		return 0, false
	}
}
