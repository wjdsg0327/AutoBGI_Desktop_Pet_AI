//go:build !windows

package storage

import "os"

// Unix 同目录 rename 保持原子替换语义。
func replace(from, to string) error { return os.Rename(from, to) }
