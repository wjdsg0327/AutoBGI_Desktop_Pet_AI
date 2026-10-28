//go:build windows

package storage

import (
	"syscall"
	"unsafe"
)

// MoveFileExW 原子替换同卷目标，WRITE_THROUGH 确保替换操作落盘。
func replace(from, to string) error {
	source, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	ok, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(source)), uintptr(unsafe.Pointer(target)), 0x1|0x8)
	if ok == 0 {
		return callErr
	}
	return nil
}
