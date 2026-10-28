package service

import "autobgi-pet/internal/profile"

// petDragPolicyChanged 仅比较影响拖动资格和坐标基准的偏好，无关显示或连接轮询不撤销手势。
func petDragPolicyChanged(before, after profile.Preferences) bool {
	return before.Pet.Visible != after.Pet.Visible ||
		before.Pet.ClickThrough != after.Pet.ClickThrough ||
		before.Pet.Scale != after.Pet.Scale ||
		before.Notifications.QuietHoursEnabled != after.Notifications.QuietHoursEnabled
}

// GetPetDragPolicy 同锁读取最新保存事实及其代次，原生拖动不依赖可能乱序或尚未消费的广播。
//
//wails:ignore
func (s *Service) GetPetDragPolicy() (uint64, profile.Preferences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.petDragGeneration, s.config.Preferences
}
