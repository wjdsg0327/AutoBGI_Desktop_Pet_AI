package service

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// NotificationRecord 只公开脱敏提醒及已读状态，尝试展示不等同于用户已经阅读。
type NotificationRecord struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Message     string  `json:"message"`
	CreatedAt   string  `json:"createdAt"`
	AttemptedAt *string `json:"attemptedAt"`
	ReadAt      *string `json:"readAt"`
}

// NotificationErrorSummary 仅表示已裁剪的未读错误，确认该摘要不会代替阅读保留正文。
type NotificationErrorSummary struct {
	Revision     string  `json:"revision"`
	UnreadCount  uint32  `json:"unreadCount"`
	QuietPending uint32  `json:"quietPending"`
	LatestAt     *string `json:"latestAt"`
}

// NotificationHistoryScope 按完整历史来源分组，旧实例绝不冒充当前在线实例。
type NotificationHistoryScope struct {
	ProfileID    string                    `json:"profileId"`
	BaseURL      string                    `json:"baseUrl"`
	SourceMode   string                    `json:"sourceMode"`
	InstanceID   string                    `json:"instanceId"`
	AccountID    *string                   `json:"accountId"`
	Entries      []NotificationRecord      `json:"entries"`
	UnreadCount  int                       `json:"unreadCount"`
	QuietPending uint32                    `json:"quietPending"`
	Truncated    bool                      `json:"truncated"`
	ErrorSummary *NotificationErrorSummary `json:"errorSummary"`
}

// NotificationHistory 在离线时仍可读取当前保存地址的历史，不改变连接或通知投递状态。
type NotificationHistory struct {
	ProfileID string                     `json:"profileId"`
	BaseURL   string                     `json:"baseUrl"`
	Scopes    []NotificationHistoryScope `json:"scopes"`
}

// NotificationReadInput 冻结用户看到的历史来源，整批标记不能跨地址或账号执行。
type NotificationReadInput struct {
	ProfileID  string   `json:"profileId"`
	BaseURL    string   `json:"baseUrl"`
	SourceMode string   `json:"sourceMode"`
	InstanceID string   `json:"instanceId"`
	AccountID  *string  `json:"accountId"`
	IDs        []string `json:"ids"`
}

// sameNotificationScope 不使用昵称、默认账号或连接代次替代持久来源身份。
func sameNotificationScope(a, b storage.NotificationScope) bool {
	accountEqual := a.AccountID == nil && b.AccountID == nil ||
		a.AccountID != nil && b.AccountID != nil && *a.AccountID == *b.AccountID
	return a.ProfileID == b.ProfileID && a.BaseURL == b.BaseURL && a.SourceMode == b.SourceMode &&
		a.InstanceID == b.InstanceID && accountEqual
}

// notificationScopeIndex 精确查找完整来源，不回落到同名连接或旧实例。
func notificationScopeIndex(j *storage.Journal, identity storage.NotificationScope) int {
	for i, scope := range j.NotificationScopes {
		if sameNotificationScope(scope, identity) {
			return i
		}
	}
	return -1
}

// notificationProfileLocked 只选当前保存的活动资料，历史列表不依赖网络在线状态。
func (s *Service) notificationProfileLocked() (profile.Profile, bool) {
	for _, p := range s.config.Profiles {
		if p.ID == s.config.ActiveProfileID {
			return p, true
		}
	}
	return profile.Profile{}, false
}

// notificationContextLocked 探测前的本地错误明确标为未核验，不能伪装成 Legacy。
func (s *Service) notificationContextLocked() (storage.NotificationScope, bool) {
	p, ok := s.activeProfileLocked()
	if !ok {
		return storage.NotificationScope{}, false
	}
	scope := storage.NotificationScope{ProfileID: p.ID, BaseURL: p.BaseURL, SourceMode: "unverified"}
	if caps := s.state.Capabilities; caps != nil {
		switch {
		case caps.SourceMode == "pet-v1" && caps.InstanceID != nil && *caps.InstanceID != "":
			scope.SourceMode, scope.InstanceID = "pet-v1", *caps.InstanceID
		case caps.SourceMode == "legacy":
			scope.SourceMode = "legacy"
		}
	} else if snapshot := s.state.Snapshot; snapshot != nil && snapshot.ProfileID == p.ID && snapshot.SourceMode == "legacy" {
		scope.SourceMode = "legacy"
	}
	return scope, true
}

// notificationErrorSummaryDTO 复制摘要及时间指针，展示对象不能反向修改已经提交的日志。
func notificationErrorSummaryDTO(summary *storage.NotificationErrorSummary) *NotificationErrorSummary {
	if summary == nil {
		return nil
	}
	result := &NotificationErrorSummary{
		Revision: summary.Revision, UnreadCount: summary.UnreadCount, QuietPending: summary.QuietPending,
	}
	if summary.LatestAt != nil {
		at := *summary.LatestAt
		result.LatestAt = &at
	}
	return result
}

// notificationHistoryLocked 只投影当前保存资料的历史，加载历史从不填入实时 Notifications。
func (s *Service) notificationHistoryLocked() *NotificationHistory {
	p, ok := s.notificationProfileLocked()
	if !ok {
		return nil
	}
	result := &NotificationHistory{ProfileID: p.ID, BaseURL: p.BaseURL, Scopes: []NotificationHistoryScope{}}
	for _, scope := range s.journal.NotificationScopes {
		if scope.ProfileID != p.ID || scope.BaseURL != p.BaseURL {
			continue
		}
		group := NotificationHistoryScope{
			ProfileID: scope.ProfileID, BaseURL: scope.BaseURL, SourceMode: scope.SourceMode,
			InstanceID: scope.InstanceID, AccountID: scope.AccountID, Entries: []NotificationRecord{},
			QuietPending: scope.QuietPending, Truncated: scope.Truncated,
			ErrorSummary: notificationErrorSummaryDTO(scope.ErrorSummary),
		}
		for _, entry := range scope.Entries {
			group.Entries = append(group.Entries, NotificationRecord{
				ID: entry.ID, Kind: entry.Kind, Message: entry.Message, CreatedAt: entry.CreatedAt,
				AttemptedAt: entry.AttemptedAt, ReadAt: entry.ReadAt,
			})
			if entry.ReadAt == nil {
				group.UnreadCount++
			}
		}
		result.Scopes = append(result.Scopes, group)
	}
	return result
}

// notificationDeliveryBlockedLocked 保留隐藏、休眠和关闭气泡时不积累补播的既有策略。
func (s *Service) notificationDeliveryBlockedLocked() bool {
	return s.notificationBlocked || s.sleepPaused || !s.config.Preferences.Pet.Visible || !s.config.Preferences.Notifications.Bubble
}

// safeNoticeMessage 仅截取已经由固定协议脱敏的展示事实，不持久保存原始响应或任意错误。
func safeNoticeMessage(message string) string {
	message = strings.TrimSpace(strings.Map(func(char rune) rune {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return ' '
		}
		return char
	}, message))
	chars := []rune(message)
	if len(chars) > 1000 {
		message = string(chars[:1000])
	}
	return message
}

// stageNotificationLocked 在调用方同一事务中入箱；只有提交成功的返回值才可交给宿主。
func (s *Service) stageNotificationLocked(next *storage.Journal, identity storage.NotificationScope, kind, message, sourceKey, createdAt string, live bool) (*Notification, error) {
	if s.journalBlocked {
		return nil, errors.New("本地提醒记录已停止写入，请修复后重新启动桌宠")
	}
	message = safeNoticeMessage(message)
	if message == "" {
		return nil, errors.New("提醒内容为空，记录未提交")
	}
	entry := storage.NotificationEntry{ID: randomID(), SourceKey: sourceKey, Kind: kind, Message: message, CreatedAt: createdAt}
	blocked := s.notificationDeliveryBlockedLocked()
	quiet := s.config.Preferences.Notifications.QuietHoursEnabled && !blocked && live
	entry.QuietPending = quiet
	// attempted 表示已保留一次交给展示管道的尝试，崩溃或宿主抑制不等于用户已读。
	if live && !quiet && !blocked {
		now := time.Now().Format(time.RFC3339Nano)
		entry.AttemptedAt = &now
	}
	inserted, err := storage.PutNotification(next, identity, entry)
	if err != nil {
		s.journalBlocked = true
		s.state.Notice = "提醒记录无法安全保存，已停止新提醒投递"
	}
	if err != nil || !inserted || !live || quiet || blocked {
		return nil, err
	}
	return &Notification{ID: entry.ID, Kind: kind, Message: message, CreatedAt: createdAt}, nil
}

// stageCommandNotificationLocked 让 HTTP 回执与 SSE 使用同一事实键，顺序交错也只产生一条提醒。
func (s *Service) stageCommandNotificationLocked(next *storage.Journal, p profile.Profile, receipt autobgi.CommandReceipt, createdAt string, live bool) (*Notification, error) {
	saved := false
	for _, current := range s.config.Profiles {
		if current.ID == p.ID && current.BaseURL == p.BaseURL {
			saved = true
			break
		}
	}
	if !saved {
		return nil, nil
	}
	for index, value := range next.Commands {
		if value.BaseURL != p.BaseURL || value.Receipt.ProfileID != p.ID ||
			value.Receipt.InstanceID != receipt.InstanceID || value.Receipt.CommandID != receipt.CommandID {
			continue
		}
		actual := value.Receipt
		if !terminalCommand(actual.Status) || value.NotificationHandled {
			return nil, nil
		}
		// 旧日志已有的已处理终态只迁移墓碑，不能通过 HTTP 查询虚构一条新提醒。
		if oldScope := eventScopeIndex(&s.journal, p, actual.InstanceID); oldScope >= 0 &&
			containsID(s.journal.Scopes[oldScope].Attempted, actual.CommandID+":"+actual.Status) {
			next.Commands[index].NotificationHandled = true
			return nil, nil
		}
		kind, message := "command", "启动动作已完成："+actual.PresetName+"；任务执行结果请查看 AutoBGI"
		if actual.Status != "succeeded" {
			kind, message = "error", "启动动作未完成："+actual.PresetName+"，请查看命令回执"
		}
		identity := storage.NotificationScope{ProfileID: p.ID, BaseURL: p.BaseURL, SourceMode: "pet-v1", InstanceID: actual.InstanceID}
		current, active := s.notificationContextLocked()
		live = live && active && sameNotificationScope(identity, current) && s.state.Connection.Status == "online"
		occurredAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, errors.New("命令回执时间无效，提醒未提交")
		}
		live = live && time.Since(occurredAt) <= 5*time.Minute && time.Until(occurredAt) < time.Minute
		for _, old := range s.journal.Commands {
			if old.BaseURL == p.BaseURL && old.Receipt.ProfileID == p.ID &&
				old.Receipt.InstanceID == actual.InstanceID && old.Receipt.CommandID == actual.CommandID &&
				terminalCommand(old.Receipt.Status) {
				// 旧版遗留终态没有可靠首次提示记录，只补历史，不把当前查询时间当成新事件。
				live = false
				break
			}
		}
		// 去重绑定在保留的命令事实中，正文被容量裁剪后也不能重新入箱或弹出。
		next.Commands[index].NotificationHandled = true
		return s.stageNotificationLocked(next, identity, kind, message,
			"command:"+actual.CommandID+":"+actual.Status, createdAt, live)
	}
	return nil, nil
}

// enqueueCommittedNoticesLocked 只接收当前进程已经成功持久化的新投递，历史绝不经此入口重放。
func (s *Service) enqueueCommittedNoticesLocked(notices ...Notification) {
	for _, notice := range notices {
		if notice.Kind == "progress" {
			for i := len(s.state.Notifications) - 1; i >= 0; i-- {
				if s.state.Notifications[i].Kind == "progress" {
					s.state.Notifications = append(s.state.Notifications[:i], s.state.Notifications[i+1:]...)
					break
				}
			}
		}
		s.state.Notifications = append(s.state.Notifications, notice)
	}
	if len(s.state.Notifications) > 20 {
		s.state.Notifications = append([]Notification(nil), s.state.Notifications[len(s.state.Notifications)-20:]...)
	}
}

// clearQuietPending 只结束汇总资格，不将提醒正文标记为已读。
func clearQuietPending(scope *storage.NotificationScope) bool {
	changed := scope.QuietPending != 0
	scope.QuietPending = 0
	// 勿扰资格与错误确认独立，结束勿扰不能清除错误未读数、修订或发生时间。
	if scope.ErrorSummary != nil && scope.ErrorSummary.QuietPending != 0 {
		scope.ErrorSummary.QuietPending = 0
		changed = true
	}
	for i := range scope.Entries {
		if scope.Entries[i].QuietPending {
			scope.Entries[i].QuietPending = false
			changed = true
		}
	}
	return changed
}

// reconcileNotificationPolicyLocked 汇总计数和摘要正文一次提交，失败既不清计数也不投递。
func (s *Service) reconcileNotificationPolicyLocked() error {
	if s.journalBlocked {
		return errors.New("本地提醒记录已停止写入，请修复后重新启动桌宠")
	}
	next := cloneJournal(s.journal)
	identity, ok := s.notificationContextLocked()
	if !ok {
		return nil
	}
	index := notificationScopeIndex(&next, identity)
	if s.notificationDeliveryBlockedLocked() {
		// 宿主禁播只消费当前精确范围，其他连接或历史实例的未读汇总资格不受影响。
		if index >= 0 && clearQuietPending(&next.NotificationScopes[index]) {
			return s.commitJournalLocked(next)
		}
		return nil
	}
	if s.config.Preferences.Notifications.QuietHoursEnabled {
		return nil
	}
	if index < 0 || next.NotificationScopes[index].QuietPending == 0 {
		return nil
	}
	count := next.NotificationScopes[index].QuietPending
	clearQuietPending(&next.NotificationScopes[index])
	message := fmt.Sprintf("勿扰已结束，期间有 %d 条未读提醒，请在通知记录中查看。", count)
	if next.NotificationScopes[index].Truncated {
		message += " 部分较早记录已超过保留容量。"
	}
	notice, err := s.stageNotificationLocked(&next, identity, "summary", message, "quiet:"+randomID(), time.Now().Format(time.RFC3339Nano), true)
	if err != nil {
		return err
	}
	if err := s.commitJournalLocked(next); err != nil {
		return err
	}
	if notice != nil {
		s.enqueueCommittedNoticesLocked(*notice)
	}
	return nil
}

// SetNotificationDeliveryBlocked 只接收 Go 宿主的临时禁播事实，不提供前端可伪造的来源输入。
//
//wails:ignore
func (s *Service) SetNotificationDeliveryBlocked(blocked bool) {
	s.mu.Lock()
	if s.closed || s.notificationBlocked == blocked {
		s.mu.Unlock()
		return
	}
	s.notificationBlocked = blocked
	if err := s.reconcileNotificationPolicyLocked(); err != nil {
		s.state.Notice = "提醒状态无法保存，已停止新提醒投递，请检查本地目录权限"
	}
	s.mu.Unlock()
	s.publish()
}

// MarkNotificationsRead 在一个精确历史范围内整批校验，保存成功后才更新已读及勿扰计数。
func (s *Service) MarkNotificationsRead(input NotificationReadInput) (AppState, error) {
	s.mu.Lock()
	if s.journalBlocked {
		s.mu.Unlock()
		return s.GetState(), errors.New("本地提醒记录已停止写入，请修复后重新启动桌宠")
	}
	p, ok := s.notificationProfileLocked()
	if s.closed || !ok || p.ID != input.ProfileID || p.BaseURL != input.BaseURL || len(input.IDs) == 0 || len(input.IDs) > storage.NotificationHistoryLimit {
		s.mu.Unlock()
		return s.GetState(), errors.New("提醒来源已经变化，请重新读取当前连接的历史")
	}
	identity := storage.NotificationScope{ProfileID: input.ProfileID, BaseURL: input.BaseURL, SourceMode: input.SourceMode, InstanceID: input.InstanceID, AccountID: input.AccountID}
	next := cloneJournal(s.journal)
	index := notificationScopeIndex(&next, identity)
	if index < 0 {
		s.mu.Unlock()
		return s.GetState(), errors.New("所选提醒历史不存在，请重新读取")
	}
	wanted := make(map[string]bool, len(input.IDs))
	for _, id := range input.IDs {
		if !validID(id) || wanted[id] {
			s.mu.Unlock()
			return s.GetState(), errors.New("提醒选择无效，请重新选择")
		}
		wanted[id] = true
	}
	scope := &next.NotificationScopes[index]
	for _, entry := range scope.Entries {
		delete(wanted, entry.ID)
	}
	if len(wanted) != 0 {
		s.mu.Unlock()
		return s.GetState(), errors.New("部分提醒已经过期，未修改任何已读状态")
	}
	for _, id := range input.IDs {
		wanted[id] = true
	}
	now := time.Now().Format(time.RFC3339Nano)
	changed := false
	for i := range scope.Entries {
		entry := &scope.Entries[i]
		if !wanted[entry.ID] || entry.ReadAt != nil {
			continue
		}
		entry.ReadAt = &now
		if entry.QuietPending {
			entry.QuietPending = false
			if scope.QuietPending > 0 {
				scope.QuietPending--
			}
		}
		changed = true
	}
	var err error
	if changed {
		err = s.commitJournalLocked(next)
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), err
}

// pruneNotificationProfilesLocked 清理删除或改地址后的孤立历史，失败时保留原文件并隐藏不匹配范围。
func (s *Service) pruneNotificationProfilesLocked() error {
	next := cloneJournal(s.journal)
	scopes := make([]storage.NotificationScope, 0, len(next.NotificationScopes))
	for _, scope := range next.NotificationScopes {
		for _, p := range s.config.Profiles {
			if p.ID == scope.ProfileID && p.BaseURL == scope.BaseURL {
				scopes = append(scopes, scope)
				break
			}
		}
	}
	// 目标历史同时绑定资料与地址；已删除或改址的资料不能留作下一次接收的水位。
	targets := make([]storage.TargetScope, 0, len(next.TargetScopes))
	for _, scope := range next.TargetScopes {
		for _, p := range s.config.Profiles {
			if p.ID == scope.ProfileID && p.BaseURL == scope.BaseURL {
				targets = append(targets, scope)
				break
			}
		}
	}
	if len(scopes) == len(next.NotificationScopes) && len(targets) == len(next.TargetScopes) {
		return nil
	}
	next.NotificationScopes = scopes
	next.TargetScopes = targets
	return s.commitJournalLocked(next)
}
