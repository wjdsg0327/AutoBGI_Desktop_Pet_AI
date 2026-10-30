//go:build windows

package desktop

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

const (
	// 原始消息分别对应鼠标和首个触摸指针的窗口激活询问，不能吞掉后续点击或拖动输入。
	mouseActivateMessage   uint32 = 0x0021
	pointerActivateMessage uint32 = 0x024B
	// MA_NOACTIVATE 与 PA_NOACTIVATE 的官方返回值都为 3，拒绝激活但继续投递输入。
	activationDeclined uintptr = 3
	// CBT 激活通知允许阻止程序调用 ShowWindow 或 SetWindowPos 时的窗口激活。
	windowActivationHookCode = 5
)

// hostWindowExStyle 仅查询当前宿主消息或 UI 线程钩子收到的窗口句柄，不枚举其他进程窗口。
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

// interceptProgrammaticActivation 只拒绝本程序 UI 线程中明确标记为不激活的窗口，设置面板仍可正常获取焦点。
func interceptProgrammaticActivation(next w32.HOOKPROC, resolveStyle func(uintptr) uintptr) w32.HOOKPROC {
	return func(code int, hwnd w32.WPARAM, data w32.LPARAM) w32.LRESULT {
		if code == windowActivationHookCode && hwnd != 0 && resolveStyle != nil &&
			resolveStyle(uintptr(hwnd))&windowExNoActivate != 0 {
			return 1
		}
		return next(code, hwnd, data)
	}
}

// guardProgrammaticActivation 在宿主 UI 线程收到首条窗口消息时安装钩子，退出后不会重新安装。
func guardProgrammaticActivation(
	next func(uintptr, uint32, uintptr, uintptr) (uintptr, bool),
	install func() w32.HHOOK,
	uninstall func(w32.HHOOK),
) (func(uintptr, uint32, uintptr, uintptr) (uintptr, bool), func()) {
	var hook w32.HHOOK
	var attempted, stopped bool
	dispatch := func(hwnd uintptr, message uint32, parameter, data uintptr) (uintptr, bool) {
		if !attempted && !stopped {
			// 提前锁存安装意图，避免原生安装期间的重入消息重复注册。
			attempted = true
			installed := install()
			// 安装期间若重入退出，不能让迟到的句柄泄漏到已关闭的宿主。
			if stopped && installed != 0 {
				uninstall(installed)
			} else {
				hook = installed
			}
		}
		return next(hwnd, message, parameter, data)
	}
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if hook != 0 {
			uninstall(hook)
			hook = 0
		}
	}
	return dispatch, stop
}

// activationGuard 接入仅属于当前 UI 线程的系统钩子，不读取或干预其他程序的窗口。
func (d *DesktopService) activationGuard(
	next func(uintptr, uint32, uintptr, uintptr) (uintptr, bool),
) func(uintptr, uint32, uintptr, uintptr) (uintptr, bool) {
	callback := interceptProgrammaticActivation(func(code int, hwnd w32.WPARAM, data w32.LPARAM) w32.LRESULT {
		return w32.CallNextHookEx(0, code, hwnd, data)
	}, hostWindowExStyle)
	dispatch, stop := guardProgrammaticActivation(next, func() w32.HHOOK {
		// Wails 的首条窗口消息已经运行在原生 UI 线程，线程钩子覆盖随后创建和显示的桌宠窗口。
		hook := w32.SetWindowsHookEx(w32.WH_CBT, callback, 0, uint32(w32.GetCurrentThreadId()))
		if hook == 0 {
			log.Print("桌宠防抢焦点初始化失败，窗口可能影响当前输入")
		}
		return hook
	}, func(hook w32.HHOOK) {
		if !w32.UnhookWindowsHookEx(hook) {
			log.Print("桌宠防抢焦点清理失败")
		}
	})
	d.stopActivationGuard = func() { application.InvokeSync(stop) }
	return dispatch
}
