package service

import "errors"

// UpdatePickupEnabled 单独保存拾取通知偏好，失败时恢复内存中的旧值。
func (s *Service) UpdatePickupEnabled(enabled bool) (AppState, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	previous := s.config.Preferences.Notifications.PickupEnabled
	s.config.Preferences.Notifications.PickupEnabled = enabled
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences.Notifications.PickupEnabled = previous
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}
