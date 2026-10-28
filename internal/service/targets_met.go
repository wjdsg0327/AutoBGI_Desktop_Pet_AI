package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// TargetsMetState 仅展示已持久接收的目标达标事实，不改变任务状态或当前培养队首。
type TargetsMetState struct {
	ProfileID        string                   `json:"profileId"`
	InstanceID       string                   `json:"instanceId"`
	ExecutionContext string                   `json:"executionContext"`
	Generation       string                   `json:"generation"`
	Cursor           string                   `json:"cursor"`
	Status           string                   `json:"status"`
	Message          string                   `json:"message"`
	Stale            bool                     `json:"stale"`
	Truncated        bool                     `json:"truncated"`
	Events           []autobgi.TargetMetEvent `json:"events"`
}

// targetScopeIndex 空执行范围只寻找最近使用的来源，用旧水位探测时仍由服务端校验范围。
func targetScopeIndex(j *storage.Journal, p profile.Profile, instanceID, executionContext string) int {
	for index := len(j.TargetScopes) - 1; index >= 0; index-- {
		scope := j.TargetScopes[index]
		if scope.ProfileID == p.ID && scope.BaseURL == p.BaseURL && scope.InstanceID == instanceID &&
			(executionContext == "" || scope.ExecutionContext == executionContext) {
			return index
		}
	}
	return -1
}

// invalidateTargetsLocked 作废已发出但未接纳的目标请求，恢复后的缺口统一只补历史。
func (s *Service) invalidateTargetsLocked(clear bool) {
	s.targetSerial++
	s.targetHistoryOnly = true
	s.targetCollecting = false
	if s.targetCancel != nil {
		s.targetCancel()
		s.targetCancel = nil
	}
	if clear {
		s.state.TargetsMet = nil
	} else if s.state.TargetsMet != nil {
		s.state.TargetsMet.Stale = true
		s.state.TargetsMet.Status = "resyncing"
	}
}

// targetsCurrentLocked 同时限制连接代次、请求串号、休眠和已验证的服务实例。
func (s *Service) targetsCurrentLocked(p profile.Profile, instanceID string, gen, serial uint64) bool {
	return s.currentLocked(gen, p.ID) && !s.paused && s.targetSerial == serial &&
		s.state.Connection.Status == "online" && s.state.Capabilities != nil &&
		s.state.Capabilities.TargetsMetEvents && s.state.Capabilities.InstanceID != nil &&
		*s.state.Capabilities.InstanceID == instanceID
}

// targetFailureLocked 保留安全历史并标记过期，失败恢复不能把遗漏事件变成新提醒。
func (s *Service) targetFailureLocked(status, message string) {
	s.targetHistoryOnly = true
	if s.state.TargetsMet == nil {
		s.state.TargetsMet = &TargetsMetState{Events: []autobgi.TargetMetEvent{}}
	}
	s.state.TargetsMet.Status = status
	s.state.TargetsMet.Message = message
	s.state.TargetsMet.Stale = true
}

// syncTargetsMet 只由唯一轮询循环调用，事件唤醒合并到该循环，不创建额外采集协程。
func (s *Service) syncTargetsMet(parent context.Context, p profile.Profile, adapter autobgi.AutoBGIAdapter, caps autobgi.Capabilities, instanceID string, gen uint64) {
	s.mu.Lock()
	if !s.currentLocked(gen, p.ID) || s.paused || s.state.Connection.Status != "online" {
		s.mu.Unlock()
		return
	}
	if !caps.TargetsMetEvents {
		s.state.TargetsMet = nil
		s.mu.Unlock()
		return
	}
	reader, ok := adapter.(autobgi.TargetsMetReader)
	if !ok {
		s.targetFailureLocked("incompatible", "当前适配器不能安全读取目标达标历史")
		s.mu.Unlock()
		s.publish()
		return
	}
	if s.targetCollecting || s.journalBlocked {
		if s.journalBlocked {
			s.targetFailureLocked("persistence_failed", "本地记录不可用，已停止目标达标历史接收与提示")
		}
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.targetSerial++
	serial := s.targetSerial
	s.targetCancel, s.targetCollecting = cancel, true
	historyOnly := s.targetHistoryOnly
	var query *autobgi.TargetsMetQuery
	if index := targetScopeIndex(&s.journal, p, instanceID, ""); index >= 0 {
		scope := s.journal.TargetScopes[index]
		query = &autobgi.TargetsMetQuery{After: scope.Cursor, Generation: scope.Generation, ExecutionContext: scope.ExecutionContext}
	} else {
		historyOnly = true
	}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if s.targetSerial == serial {
			s.targetCancel, s.targetCollecting = nil, false
		}
		s.mu.Unlock()
	}()
	// 每轮最多接收十页，超大恢复缺口留到下次轮询，期间所有页面保持只补历史。
	resynced := false
	for pageIndex := 0; pageIndex < 10 && ctx.Err() == nil; pageIndex++ {
		page, err := reader.ReadTargetsMet(ctx, query)
		if err == nil {
			err = autobgi.ValidateTargetsMetPage(page, instanceID, query)
		}
		s.mu.Lock()
		if ctx.Err() != nil || !s.targetsCurrentLocked(p, instanceID, gen, serial) {
			s.mu.Unlock()
			return
		}
		if err != nil {
			var apiErr *autobgi.APIError
			if errors.As(err, &apiErr) && apiErr.Code == "RESYNC_REQUIRED" && query != nil && !resynced {
				// 新代次改读原子历史基线，保留稳定业务去重记录，不重放气泡。
				query, historyOnly, resynced = nil, true, true
				s.targetHistoryOnly = true
				if s.state.TargetsMet != nil {
					s.state.TargetsMet.Stale = true
					s.state.TargetsMet.Status = "resyncing"
					s.state.TargetsMet.Message = "目标达标记录范围已变化，正在重新建立历史基线"
				}
				s.mu.Unlock()
				s.publish()
				continue
			}
			status, message := "backoff", "目标达标历史暂时不可用，恢复后只补历史"
			if errors.As(err, &apiErr) {
				switch apiErr.Code {
				case "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE", "NOT_FOUND", "INVALID_QUERY":
					status, message = "incompatible", "目标达标历史协议不兼容，未接收未经核实的记录"
				case "AUTH_INVALID":
					status, message = "auth_failed", "目标达标历史鉴权失败，请检查当前连接"
					s.state.Connection.Status = "auth_failed"
					s.state.Connection.ErrorCode, s.state.Connection.ErrorMessage = "AUTH_INVALID", message
					s.state.PreparedCommand = nil
					if s.state.Snapshot != nil {
						s.state.Snapshot.Stale = true
					}
					if s.cancel != nil {
						s.cancel()
					}
				}
			}
			s.targetFailureLocked(status, message)
			s.mu.Unlock()
			s.publish()
			return
		}
		// 无查询的首次页始终只补历史；续页保留本次恢复过程的抑制语义。
		err = s.acceptTargetsMetLocked(p, page, historyOnly || query == nil, time.Now())
		if err != nil {
			s.targetFailureLocked("persistence_failed", "目标达标记录未能安全保存，未展示新的目标达标提示")
			s.mu.Unlock()
			s.publish()
			return
		}
		if !page.HasMore {
			s.targetHistoryOnly = false
			s.mu.Unlock()
			s.publish()
			return
		}
		// 多页说明采集中存在积压，之后的页面不能被当作实时到达逐条提醒。
		historyOnly = true
		s.targetHistoryOnly = true
		query = &autobgi.TargetsMetQuery{After: page.Cursor, Generation: page.Generation, ExecutionContext: page.ExecutionContext}
		s.mu.Unlock()
		s.publish()
	}
}

// trimTargetSeen 只淘汰已超出可展示时间的记录，新鲜窗口过载时拒绝推进水位。
func trimTargetSeen(items []storage.TargetSeen, now time.Time) ([]storage.TargetSeen, bool) {
	for len(items) > storage.TargetSeenLimit {
		victim := -1
		for index, item := range items {
			occurred, err := time.Parse(time.RFC3339Nano, item.OccurredAt)
			if err == nil && now.Sub(occurred) > 6*time.Minute {
				victim = index
				break
			}
		}
		if victim < 0 {
			return nil, false
		}
		items = append(items[:victim], items[victim+1:]...)
	}
	return items, true
}

// targetMessage 只说明面板观察已达标，不能声称是本次派发造成的游戏培养结果。
func targetMessage(event autobgi.TargetMetEvent) string {
	if event.Kind == autobgi.TargetQueueMet {
		return "「" + event.ConfigName + "」原培养队列的全部基础等级与天赋目标已由可信面板核实；这不表示本次游戏运行完成培养"
	}
	return "「" + event.CharacterName + "」的基础等级与天赋目标已由可信面板核实；这不表示本次游戏运行完成培养"
}

// applyTargetHistoryLocked 只从已提交范围构造展示状态，空页同步无需反复写入同一份磁盘记录。
func (s *Service) applyTargetHistoryLocked(p profile.Profile, scope storage.TargetScope) {
	s.state.TargetsMet = &TargetsMetState{ProfileID: p.ID, InstanceID: scope.InstanceID,
		ExecutionContext: scope.ExecutionContext, Generation: scope.Generation, Cursor: scope.Cursor, Status: "online",
		Truncated: scope.Truncated, Events: append([]autobgi.TargetMetEvent{}, scope.History...)}
}

// acceptTargetsMetLocked 一次提交水位、去重、历史与展示处理标记，成功之后才发布通知。
func (s *Service) acceptTargetsMetLocked(p profile.Profile, page autobgi.TargetsMetPage, historyOnly bool, now time.Time) error {
	if index := targetScopeIndex(&s.journal, p, page.InstanceID, page.ExecutionContext); index >= 0 && index == len(s.journal.TargetScopes)-1 {
		scope := s.journal.TargetScopes[index]
		if len(page.Events) == 0 && scope.Generation == page.Generation && scope.Cursor == page.Cursor && (!page.Truncated || scope.Truncated) {
			s.applyTargetHistoryLocked(p, scope)
			return nil
		}
	}
	next := cloneJournal(s.journal)
	index := targetScopeIndex(&next, p, page.InstanceID, page.ExecutionContext)
	if index < 0 {
		if len(next.TargetScopes) >= storage.TargetScopeLimit {
			next.TargetScopes = append([]storage.TargetScope(nil), next.TargetScopes[1:]...)
		}
		next.TargetScopes = append(next.TargetScopes, storage.TargetScope{ProfileID: p.ID, BaseURL: p.BaseURL,
			InstanceID: page.InstanceID, ExecutionContext: page.ExecutionContext, History: []autobgi.TargetMetEvent{}, Seen: []storage.TargetSeen{}})
		index = len(next.TargetScopes) - 1
	} else if index != len(next.TargetScopes)-1 {
		// 最近接收的执行范围放在末尾，切回旧目录后不能每次都探测另一目录的水位。
		value := next.TargetScopes[index]
		next.TargetScopes = append(next.TargetScopes[:index], next.TargetScopes[index+1:]...)
		next.TargetScopes = append(next.TargetScopes, value)
		index = len(next.TargetScopes) - 1
	}
	scope := &next.TargetScopes[index]
	seen := make(map[string]storage.TargetSeen, len(scope.Seen))
	for _, item := range scope.Seen {
		seen[item.BusinessEventID] = item
	}
	history := make(map[string]autobgi.TargetMetEvent, len(scope.History))
	for _, event := range scope.History {
		history[event.BusinessEventID] = event
	}
	notices := []Notification{}
	newHistory := []autobgi.TargetMetEvent{}
	for _, event := range page.Events {
		fingerprint := storage.TargetFingerprint(event)
		if known, ok := seen[event.BusinessEventID]; ok {
			if known.Fingerprint != fingerprint || known.OccurredAt != event.OccurredAt {
				return errors.New("同一目标达标业务身份对应矛盾事实")
			}
			continue
		}
		if known, ok := history[event.BusinessEventID]; ok {
			if storage.TargetFingerprint(known) != fingerprint {
				return errors.New("目标达标历史身份发生冲突")
			}
			continue
		}
		// 历史补偿与过期事件也记作已处理，之后换传输代次不能补弹。
		item := storage.TargetSeen{BusinessEventID: event.BusinessEventID, Fingerprint: fingerprint,
			OccurredAt: event.OccurredAt, PresentationHandled: true}
		scope.Seen = append(scope.Seen, item)
		seen[event.BusinessEventID] = item
		scope.History = append(scope.History, event)
		history[event.BusinessEventID] = event
		newHistory = append(newHistory, event)
	}
	var valid bool
	scope.Seen, valid = trimTargetSeen(scope.Seen, now)
	if !valid {
		return errors.New("目标达标提示去重窗口已满，停止接收以避免重复提示")
	}
	if len(scope.History) > storage.TargetHistoryLimit {
		scope.History = append([]autobgi.TargetMetEvent(nil), scope.History[len(scope.History)-storage.TargetHistoryLimit:]...)
		scope.Truncated = true
	}
	scope.Generation, scope.Cursor = page.Generation, page.Cursor
	scope.Truncated = scope.Truncated || page.Truncated
	// 目标达标事实、接收水位与普通提醒正文共用同次 journal 提交，历史补偿只入箱不补弹。
	identity := storage.NotificationScope{ProfileID: p.ID, BaseURL: p.BaseURL, SourceMode: "pet-v1", InstanceID: page.InstanceID}
	for _, event := range newHistory {
		occurred, _ := time.Parse(time.RFC3339Nano, event.OccurredAt)
		live := !historyOnly && !page.HasMore && now.Sub(occurred) <= 5*time.Minute && occurred.Sub(now) < time.Minute
		notice, err := s.stageNotificationLocked(&next, identity, "targets", targetMessage(event), "target:"+event.BusinessEventID, event.OccurredAt, live)
		if err != nil {
			return err
		}
		if notice != nil {
			notices = append(notices, *notice)
		}
	}
	if err := s.commitJournalLocked(next); err != nil {
		return err
	}
	s.applyTargetHistoryLocked(p, s.journal.TargetScopes[index])
	// 同批提醒超出展示容量时保留摘要，完整事实仍可在目标达标历史中查看。
	if len(notices) > 20 {
		last := notices[len(notices)-1]
		summary := Notification{ID: last.ID + ":summary", Kind: "targets", CreatedAt: last.CreatedAt,
			Message: fmt.Sprintf("另有 %d 条目标达标提醒已合并，请查看达标观察与提交时间；这不表示本次游戏运行完成培养", len(notices)-19)}
		notices = append(append([]Notification(nil), notices[len(notices)-19:]...), summary)
	}
	// 接收时间不替代业务发生时间，重启也不重新把持久历史灌入通知队列。
	s.enqueueCommittedNoticesLocked(notices...)
	return nil
}
