//go:build windows

package desktop

import "github.com/wailsapp/wails/v3/pkg/w32"

const (
	// 原始消息分别对应鼠标和首个触摸指针的窗口激活询问，不能吞掉后续点击或拖动输入。
	mouseActivateMessage   uint32 = 0x0021
	pointerActivateMessage uint32 = 0x024B
	// MA_NOACTIVATE 与 PA_NOACTIVATE 的官方返回值都为 3，拒绝激活但继续投递输入。
	activationDeclined uintptr = 3
)

// hostWindowExStyle 仅查询当前宿主 WndProcInterceptor 收到的窗口句柄，不枚举或读取其他进程窗口。
func hostWindowExStyle(hwnd uintptr) uintptr {
	return w32.GetWindowLongPtr(w32.HWND(hwnd), w32.GWL_EXSTYLE)
}

// interceptWindowActivation 补足系统悬停激活绕过扩展样式的情况，面板和原电源消息链继续交给宿主。
func interceptWindowActivation(next func(uintptr, uint32, uintptr, uintptr) (uintptr, bool),
	resolveStyle func(uintptr) uintptr,
) func(uintptr, uint32, uintptr, uintptr) (uintptr, bool) {
	return func(hwnd uintptr, message uint32, parameter, data uintptr) (uintptr, bool) {
		// 先筛选激活消息再读取样式，普通输入和电源消息不产生额外原生查询。
		if message == mouseActivateMessage || message == pointerActivateMessage {
			if hwnd != 0 && resolveStyle != nil && resolveStyle(hwnd)&windowExNoActivate != 0 {
				return activationDeclined, true
			}
		}
		if next != nil {
			return next(hwnd, message, parameter, data)
		}
		return 0, false
	}
}
