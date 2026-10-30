package service

import (
	"errors"
	"time"

	"autobgi-pet/internal/autobgi"
)

// shutdownFromTodoForConnection 只向用户确认的原连接发送一次固定关机请求。
func (s *Service) shutdownFromTodoForConnection(profileID string, generation uint64) error {
	s.mu.Lock()
	current := s.menuConnectionMatchesLocked(profileID, profileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，未发送关机请求")
	}
	if !s.setHotKeyPressing(true) {
		return oneLongMenuError("TASK_BUSY", "已有控制请求正在处理，未发送关机请求")
	}
	defer s.setHotKeyPressing(false)
	if err := s.ensureMenuAdapter(); err != nil {
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "当前连接不可用，未发送关机请求")
	}
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(profileID, profileID, generation) {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，未发送关机请求")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(70 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "当前连接不可用，未发送关机请求")
	}
	controller, ok := ticket.adapter.(autobgi.ComputerShutdownController)
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, profileID, generation)
	s.mu.Unlock()
	defer release()
	if !ok {
		return oneLongMenuError("UNSUPPORTED_CAPABILITY", "当前连接未提供关机接口")
	}
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，未发送关机请求")
	}
	if err := controller.ShutdownComputer(ctx); err != nil {
		var apiErr *autobgi.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != 408 {
			return oneLongMenuError(apiErr.Code, apiErr.Message)
		}
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "关机请求结果未知，请查看 AutoBGI 日志核对；不会自动重发", Unknown: true}
	}
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, profileID, generation)
	s.mu.Unlock()
	if !current {
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "连接已变化，请查看原 AutoBGI 日志核对关机请求；不会自动重发", Unknown: true}
	}
	return nil
}
