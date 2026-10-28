//go:build !windows

package desktop

import "os"

// replacePosition 在其他平台使用同目录重命名；首发 GUI 仍只验收 Windows。
func replacePosition(from, to string) error { return os.Rename(from, to) }
