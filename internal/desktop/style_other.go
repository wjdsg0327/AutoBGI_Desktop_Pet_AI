//go:build !windows

package desktop

// petExStyle 其他系统忽略 Windows 扩展样式，未宣称已完成跨平台 GUI 验收。
func petExStyle(top bool) int { return 0 }
