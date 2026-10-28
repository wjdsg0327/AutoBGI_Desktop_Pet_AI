//go:build !windows

package platform

import "errors"

// SetLaunchOnLogin 在未适配的平台明确拒绝启用，不静默伪装成已生效。
func SetLaunchOnLogin(enabled bool) error {
	if enabled {
		return errors.New("当前平台尚未提供开机启动")
	}
	return nil
}
