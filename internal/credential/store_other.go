//go:build !windows

package credential

// 非 Windows 构建只允许显式会话凭据，不假装已有系统凭据保护。
type unavailableStore struct{}

func New() Store                                     { return unavailableStore{} }
func (unavailableStore) Read(string) (string, error) { return "", ErrUnavailable }
func (unavailableStore) Write(string, string) error  { return ErrUnavailable }

// 非 Windows 没有本程序的持久凭据，删除视为空操作以允许显式会话模式。
func (unavailableStore) Delete(string) error { return nil }
