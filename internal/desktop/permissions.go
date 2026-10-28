package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// deniedPermissions 禁止只读桌宠使用摄像头、麦克风、定位、网页通知或剪贴板读取。
func deniedPermissions() map[application.PermissionType]application.Permission {
	return map[application.PermissionType]application.Permission{
		application.PermissionMicrophone:    application.PermissionDeny,
		application.PermissionCamera:        application.PermissionDeny,
		application.PermissionGeolocation:   application.PermissionDeny,
		application.PermissionNotifications: application.PermissionDeny,
		application.PermissionClipboardRead: application.PermissionDeny,
	}
}

// deniedWindowsPermissions 同时拒绝 Windows 特有的传感器和未知权限，避免框架默认全允许。
func deniedWindowsPermissions() map[application.CoreWebView2PermissionKind]application.CoreWebView2PermissionState {
	permissions := make(map[application.CoreWebView2PermissionKind]application.CoreWebView2PermissionState)
	for kind := application.CoreWebView2PermissionKindUnknownPermission; kind <= application.CoreWebView2PermissionKindClipboardRead; kind++ {
		permissions[kind] = application.CoreWebView2PermissionStateDeny
	}
	return permissions
}
