package service

import (
	"errors"

	"autobgi-pet/internal/profile"
)

// UpdateKeywordSoundEnabled 兼容旧版本全局音效开关，不参与当前规则的独立触发判断。
func (s *Service) UpdateKeywordSoundEnabled(enabled bool) (AppState, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	previous := s.config.Preferences.Notifications.KeywordSoundEnabled
	s.config.Preferences.Notifications.KeywordSoundEnabled = enabled
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences.Notifications.KeywordSoundEnabled = previous
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// UpdateKeywordSoundType 兼容旧版本全局音效类型，当前规则保存自己的音效类型。
func (s *Service) UpdateKeywordSoundType(soundType string) (AppState, error) {
	if !profile.ValidKeywordSoundType(soundType) {
		return s.GetState(), errors.New("关键词音效类型无效")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	if soundType == profile.KeywordSoundTypeCustom && !s.store.ValidKeywordSound() {
		s.mu.Unlock()
		return s.GetState(), errors.New("自定义音效文件不存在或已损坏，请重新选择")
	}
	previous := s.config.Preferences.Notifications.KeywordSoundType
	s.config.Preferences.Notifications.KeywordSoundType = soundType
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences.Notifications.KeywordSoundType = previous
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// UpdateKeywordCustomSound 复制并校验用户 WAV 音效，同时切换到自定义音效。
func (s *Service) UpdateKeywordCustomSound(source string) (AppState, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	previousType := s.config.Preferences.Notifications.KeywordSoundType
	previousName := s.config.Preferences.Notifications.KeywordSoundCustomName
	previousRules := append([]profile.KeywordRule(nil), s.config.KeywordRules...)
	previousData, previousExists := s.store.ReadKeywordSound()
	name, err := s.store.SaveKeywordSound(source)
	if err != nil {
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.config.Preferences.Notifications.KeywordSoundType = profile.KeywordSoundTypeCustom
	s.config.Preferences.Notifications.KeywordSoundCustomName = name
	for i := range s.config.KeywordRules {
		if s.config.KeywordRules[i].SoundType == profile.KeywordSoundTypeCustom {
			s.config.KeywordRules[i].SoundCustomName = name
		}
	}
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences.Notifications.KeywordSoundType = previousType
		s.config.Preferences.Notifications.KeywordSoundCustomName = previousName
		s.config.KeywordRules = previousRules
		_ = s.store.RestoreKeywordSound(previousData, previousExists)
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// ClearKeywordCustomSound 删除内部 WAV 并回退到安静陪伴语音。
func (s *Service) ClearKeywordCustomSound() (AppState, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	previousType := s.config.Preferences.Notifications.KeywordSoundType
	previousName := s.config.Preferences.Notifications.KeywordSoundCustomName
	previousRules := append([]profile.KeywordRule(nil), s.config.KeywordRules...)
	previousData, previousExists := s.store.ReadKeywordSound()
	if err := s.store.RemoveKeywordSound(); err != nil {
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.config.Preferences.Notifications.KeywordSoundType = profile.DefaultKeywordSoundType
	s.config.Preferences.Notifications.KeywordSoundCustomName = ""
	for i := range s.config.KeywordRules {
		if s.config.KeywordRules[i].SoundType == profile.KeywordSoundTypeCustom {
			s.config.KeywordRules[i].SoundType = profile.DefaultKeywordSoundType
			s.config.KeywordRules[i].SoundCustomName = ""
		}
	}
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences.Notifications.KeywordSoundType = previousType
		s.config.Preferences.Notifications.KeywordSoundCustomName = previousName
		s.config.KeywordRules = previousRules
		_ = s.store.RestoreKeywordSound(previousData, previousExists)
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// KeywordSoundPath 返回自定义音效的内部路径，仅供原生播放实现使用。
func (s *Service) KeywordSoundPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.KeywordSoundPath()
}

// KeywordSoundAvailable 只返回内部自定义 WAV 是否仍可安全播放，不暴露文件路径。
func (s *Service) KeywordSoundAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.ValidKeywordSound()
}
