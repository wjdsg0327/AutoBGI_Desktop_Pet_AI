package service

import (
	"errors"

	"autobgi-pet/internal/storage"
)

// NotificationErrorAcknowledgeInput 冻结所见来源和摘要修订，迟到确认不能吞掉后续裁剪的新错误。
type NotificationErrorAcknowledgeInput struct {
	ProfileID  string  `json:"profileId"`
	BaseURL    string  `json:"baseUrl"`
	SourceMode string  `json:"sourceMode"`
	InstanceID string  `json:"instanceId"`
	AccountID  *string `json:"accountId"`
	Revision   string  `json:"revision"`
}

// AcknowledgeNotificationErrors 只确认已裁剪错误的精确版本，保留正文及其已读状态完全独立。
func (s *Service) AcknowledgeNotificationErrors(input NotificationErrorAcknowledgeInput) (AppState, error) {
	s.mu.Lock()
	if s.journalBlocked {
		s.mu.Unlock()
		return s.GetState(), errors.New("本地提醒记录已停止写入，请修复后重新启动桌宠")
	}
	stale := errors.New("错误摘要已经变化，请重新读取当前连接的通知记录后再确认")
	p, ok := s.notificationProfileLocked()
	if s.closed || !ok || p.ID != input.ProfileID || p.BaseURL != input.BaseURL || input.AccountID != nil {
		s.mu.Unlock()
		return s.GetState(), stale
	}
	identity := storage.NotificationScope{
		ProfileID: input.ProfileID, BaseURL: input.BaseURL, SourceMode: input.SourceMode,
		InstanceID: input.InstanceID, AccountID: input.AccountID,
	}
	next := cloneJournal(s.journal)
	index := notificationScopeIndex(&next, identity)
	if index < 0 || next.NotificationScopes[index].ErrorSummary == nil ||
		next.NotificationScopes[index].ErrorSummary.UnreadCount == 0 ||
		next.NotificationScopes[index].ErrorSummary.Revision != input.Revision {
		s.mu.Unlock()
		return s.GetState(), stale
	}
	// 存储层在独立副本中同时推进全局修订并扣除摘要自己的勿扰数，失败不影响正文或旧摘要。
	if err := storage.AcknowledgeNotificationErrors(&next, identity, input.Revision); err != nil {
		s.mu.Unlock()
		return s.GetState(), err
	}
	err := s.commitJournalLocked(next)
	s.mu.Unlock()
	s.publish()
	return s.GetState(), err
}
