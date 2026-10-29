package service

import (
	"errors"
	"time"

	"autobgi-pet/internal/autobgi"
)

// closeMenuRequestError 区分明确拒绝和可能已经执行的关闭请求，不自动重发。
func closeMenuRequestError(err error) error {
	var apiErr *autobgi.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != 408 {
		switch apiErr.Code {
		case "NETWORK_ERROR", "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE":
		default:
			return &OneLongStartError{Code: apiErr.Code, Message: apiErr.Message}
		}
	}
	return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "关闭请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
}

// CloseFromMenuForConnection 只允许原生菜单向已冻结的在线连接单次发送固定关闭操作。
//
//wails:ignore
func (s *Service) CloseFromMenuForConnection(action, profileID, activeProfileID string, generation uint64) error {
	if action != "bgi_and_game" && action != "game" {
		return oneLongMenuError("INVALID_ARGUMENT", "关闭操作无效")
	}
	s.mu.Lock()
	current := s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单")
	}
	// 与快捷键发送共用门禁，避免一条关闭操作尚未完成时重复触发。
	if !s.setHotKeyPressing(true) {
		return oneLongMenuError("TASK_BUSY", "已有控制请求正在处理，请稍候")
	}
	defer s.setHotKeyPressing(false)
	if err := s.ensureMenuAdapter(); err != nil {
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "当前连接不可用，请重新连接")
	}
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(profileID, activeProfileID, generation) {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单")
	}
	// 关闭接口可能等待进程退出，保留充分时间并跟随连接和暂停周期取消。
	ticket, ctx, release, err := s.beginManualReadLocked(40 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "当前连接不可用，请重新连接")
	}
	controller, ok := ticket.adapter.(autobgi.CloseController)
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	defer release()
	if !ok {
		return oneLongMenuError("UNSUPPORTED_CAPABILITY", "当前连接未提供关闭接口")
	}
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单")
	}

	// 只向冻结的适配器派发一次；服务端可能已执行，即使响应未到达。
	if action == "game" {
		err = controller.CloseGame(ctx)
	} else {
		err = controller.CloseBGIAndGame(ctx)
	}
	if err != nil {
		return closeMenuRequestError(err)
	}
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "连接已变化，关闭请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
	}
	s.Refresh()
	return nil
}
