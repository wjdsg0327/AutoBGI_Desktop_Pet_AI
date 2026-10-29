package service

import (
	"errors"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
)

// bgiMenuHotKeyNames 限定原生菜单只可调用服务端已定义的六项系统快捷键。
var bgiMenuHotKeyNames = map[string]bool{
	"启动停止BetterGI": true,
	"停止当前脚本/ 独立任务": true,
	"暂停当前脚本/ 独立任务": true,
	"游戏截图":         true,
	"日志与状态窗口展示开关":  true,
	"启动停止一条龙":      true,
}

// ReadBGIHotKeysForConnection 只读取菜单创建时绑定的在线连接，拒绝迟到的旧结果。
//
//wails:ignore
func (s *Service) ReadBGIHotKeysForConnection(profileID, activeProfileID string, generation uint64) (map[string]string, error) {
	s.mu.Lock()
	current := s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return nil, errors.New("连接已变化，请重新打开菜单")
	}
	if err := s.ensureMenuAdapter(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(profileID, activeProfileID, generation) {
		s.mu.Unlock()
		return nil, errors.New("连接已变化，请重新打开菜单")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(10 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	controller, ok := ticket.adapter.(autobgi.HotKeyController)
	s.mu.Unlock()
	defer release()
	if !ok {
		return nil, errors.New("当前连接未提供 BGI 快捷键接口")
	}
	keys, err := controller.ReadBGIHotKeys(ctx)
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return nil, errors.New("读取期间连接已变化或请求超时，请重新打开菜单")
	}
	if err != nil {
		return nil, err
	}
	// 仅向菜单返回已知项目，避免将远端额外字段转成可执行入口。
	result := make(map[string]string, len(bgiMenuHotKeyNames))
	for name := range bgiMenuHotKeyNames {
		result[name] = keys[name]
	}
	return result, nil
}

// setHotKeyPressing 使未完成的按键请求不能被重复菜单点击并发派发。
func (s *Service) setHotKeyPressing(pressing bool) bool {
	s.hotKeyPressMu.Lock()
	defer s.hotKeyPressMu.Unlock()
	if pressing && s.hotKeyPressing {
		return false
	}
	s.hotKeyPressing = pressing
	return true
}

// hotKeyRequestError 区分明确拒绝与可能已经派发的未知结果，禁止自动重发。
func hotKeyRequestError(err error) error {
	var apiErr *autobgi.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != 408 {
		switch apiErr.Code {
		case "NETWORK_ERROR", "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE":
		default:
			return &OneLongStartError{Code: apiErr.Code, Message: apiErr.Message}
		}
	}
	return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "按键请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
}

// PressBGIHotKeyForConnection 重新核实配置和来源后，只发送一次固定快捷键请求。
//
//wails:ignore
func (s *Service) PressBGIHotKeyForConnection(name, key, profileID, activeProfileID string, generation uint64) error {
	if !bgiMenuHotKeyNames[name] || strings.TrimSpace(key) == "" {
		return oneLongMenuError("INVALID_ARGUMENT", "快捷键未配置，请重新打开菜单")
	}
	s.mu.Lock()
	current := s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单")
	}
	if !s.setHotKeyPressing(true) {
		return oneLongMenuError("TASK_BUSY", "已有按键请求正在处理，请稍候")
	}
	defer s.setHotKeyPressing(false)
	if err := s.ensureMenuAdapter(); err != nil {
		return oneLongMenuError("CONNECTION_UNAVAILABLE", err.Error())
	}
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(profileID, activeProfileID, generation) {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(20 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_UNAVAILABLE", err.Error())
	}
	controller, ok := ticket.adapter.(autobgi.HotKeyController)
	s.mu.Unlock()
	defer release()
	if !ok {
		return oneLongMenuError("UNSUPPORTED_CAPABILITY", "当前连接未提供 BGI 快捷键接口")
	}

	// 点击时重新读取配置，防止菜单展示期间快捷键被修改后误按旧键。
	keys, err := controller.ReadBGIHotKeys(ctx)
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化或读取超时，请重新打开菜单")
	}
	if err != nil {
		return oneLongMenuError("LIST_FAILED", "无法读取 BGI 快捷键，请重新打开菜单")
	}
	if keys[name] != key || strings.TrimSpace(keys[name]) == "" {
		return oneLongMenuError("HOTKEY_CHANGED", "快捷键配置已变化，请重新打开菜单")
	}

	// 仅向已冻结的适配器派发一次；超时或断线不能证明服务端未收到请求。
	if err := controller.PressBGIHotKey(ctx, key); err != nil {
		return hotKeyRequestError(err)
	}
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "连接已变化，按键请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
	}
	return nil
}
