//go:build windows

package desktop

import "golang.org/x/sys/windows"

// replacePosition 使用 Windows 同卷替换语义，避免 Rename 的已存在目标限制。
func replacePosition(from, to string) error {
	a, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	b, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
