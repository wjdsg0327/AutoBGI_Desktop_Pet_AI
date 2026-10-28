//go:build windows

// Package platform 只提供宿主内部的系统能力，不注册任意文件或进程调用绑定。
package platform

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// SetLaunchOnLogin 仅在用户明确启用后写当前用户 Run，不需要管理员权限。
func SetLaunchOnLogin(enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return errors.New("无法修改当前用户的开机启动设置")
	}
	defer key.Close()
	if !enabled {
		if err := key.DeleteValue("AutoBGI-Pet"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return errors.New("无法关闭开机启动")
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil || strings.ContainsRune(exe, '"') {
		return errors.New("无法确定桌宠程序位置")
	}
	if err := key.SetStringValue("AutoBGI-Pet", `"`+exe+`"`); err != nil {
		return errors.New("无法开启开机启动")
	}
	return nil
}
