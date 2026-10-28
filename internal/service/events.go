package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// eventScopeIndex 以完整来源身份定位持久游标，未知或其他实例不能复用。
func eventScopeIndex(j *storage.Journal, p profile.Profile, instanceID string) int {
	for i, scope := range j.Scopes {
		if scope.ProfileID == p.ID && scope.BaseURL == p.BaseURL && scope.InstanceID == instanceID {
			return i
		}
	}
	return -1
}

// appendBounded 使去重窗口有明确上限，持久化不会随运行时间无限增大。
func appendBounded(values []string, value string) []string {
	values = append(values, value)
	if len(values) > 256 {
		values = append([]string(nil), values[len(values)-256:]...)
	}
	return values
}

// containsID 对小型有界去重窗口进行精确比较，不采用哈希碰撞概率判断事实。
func containsID(values []string, id string) bool {
	for _, value := range values {
		if value == id {
			return true
		}
	}
	return false
}

// ensureEventScopeLocked 创建事件基线时也先落盘，完整快照不会转化为任务完成通知。
func (s *Service) ensureEventScopeLocked(p profile.Profile, snapshot autobgi.Snapshot, reset bool) (*string, error) {
	if snapshot.ServerInstanceID == nil || snapshot.EventCursor == nil || *snapshot.EventCursor == "" {
		return nil, errors.New("服务端没有返回可恢复的事件游标")
	}
	next := cloneJournal(s.journal)
	index := eventScopeIndex(&next, p, *snapshot.ServerInstanceID)
	if index >= 0 && !reset && next.Scopes[index].Cursor != "" {
		cursor := next.Scopes[index].Cursor
		return &cursor, nil
	}
	if index < 0 {
		if len(next.Scopes) >= 40 {
			next.Scopes = append([]storage.EventScope(nil), next.Scopes[1:]...)
		}
		next.Scopes = append(next.Scopes, storage.EventScope{ProfileID: p.ID, BaseURL: p.BaseURL, InstanceID: *snapshot.ServerInstanceID, Seen: []string{}, Attempted: []string{}})
		index = len(next.Scopes) - 1
	}
	next.Scopes[index].Cursor = *snapshot.EventCursor
	if err := s.commitJournalLocked(next); err != nil {
		return nil, err
	}
	cursor := next.Scopes[index].Cursor
	return &cursor, nil
}

// startEventsLocked 每个连接代次最多启动一条订阅，睡眠恢复使用新的串号拒绝旧流回调。
func (s *Service) startEventsLocked(parent context.Context, p profile.Profile, adapter autobgi.AutoBGIAdapter, caps autobgi.Capabilities, snapshot autobgi.Snapshot, gen uint64) {
	if !caps.Events {
		s.state.EventStatus = "unsupported"
		return
	}
	if s.eventStarted || s.paused {
		return
	}
	if s.journalBlocked {
		s.state.EventStatus = "persistence_failed"
		return
	}
	cursor, err := s.ensureEventScopeLocked(p, snapshot, false)
	if err != nil {
		s.state.EventStatus = "persistence_failed"
		s.state.Notice = err.Error()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.eventCancel = cancel
	s.eventStarted = true
	s.eventSerial++
	serial := s.eventSerial
	s.state.EventStatus = "connecting"
	s.wg.Add(1)
	go s.eventLoop(ctx, p, adapter, *snapshot.ServerInstanceID, cursor, gen, serial)
}

// eventLoop 只恢复同一身份的持久游标；游标失效通过完整快照重新建立基线。
func (s *Service) eventLoop(ctx context.Context, p profile.Profile, adapter autobgi.AutoBGIAdapter, instanceID string, cursor *string, gen, serial uint64) {
	defer s.wg.Done()
	failures := 0
	for ctx.Err() == nil {
		err := adapter.Subscribe(ctx, autobgi.EventQuery{Cursor: cursor}, func(event autobgi.Event) {
			s.mu.Lock()
			if !s.currentLocked(gen, p.ID) || s.eventSerial != serial || s.paused || event.InstanceID != instanceID {
				s.mu.Unlock()
				return
			}
			changed, persistErr := s.acceptEventLocked(p, event)
			if persistErr != nil {
				s.state.EventStatus = "persistence_failed"
				if s.eventCancel != nil {
					s.eventCancel()
				}
			} else {
				s.state.EventStatus = "online"
				if changed {
					value := event.EventID
					cursor = &value
				}
			}
			s.mu.Unlock()
			if persistErr == nil && changed && (event.Type == "snapshot.updated" || event.Type == "server.notice") {
				s.Refresh()
			}
			s.publish()
		})
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		if !s.currentLocked(gen, p.ID) || s.eventSerial != serial {
			s.mu.Unlock()
			return
		}
		var apiErr *autobgi.APIError
		code := "NETWORK_ERROR"
		if errors.As(err, &apiErr) {
			code = apiErr.Code
		}
		switch code {
		case "AUTH_INVALID":
			s.state.EventStatus = "auth_failed"
			s.state.Connection.Status = "auth_failed"
			s.state.Connection.ErrorCode, s.state.Connection.ErrorMessage = apiErr.Code, apiErr.Message
			s.state.PreparedCommand = nil
			if s.state.Snapshot != nil {
				s.state.Snapshot.Stale = true
			}
			if s.cancel != nil {
				s.cancel()
			}
			s.mu.Unlock()
			s.publish()
			return
		case "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE", "REDIRECT_REJECTED", "NOT_FOUND":
			s.state.EventStatus = "incompatible"
			s.state.Notice = "事件流不可用，继续使用轮询快照；不会生成未经核实的完成通知"
			s.mu.Unlock()
			s.publish()
			return
		case "RESYNC_REQUIRED":
			s.state.EventStatus = "resyncing"
			s.invalidateMaterialsLocked(false)
			s.invalidateTargetsLocked(false)
			// 过期 SSE 只唤醒统一采集循环，补偿历史不会生成新气泡。
			select {
			case s.refresh <- struct{}{}:
			default:
			}
		default:
			s.state.EventStatus = "backoff"
			s.invalidateMaterialsLocked(false)
			s.invalidateTargetsLocked(false)
		}
		s.mu.Unlock()
		s.publish()
		if code == "RESYNC_REQUIRED" {
			snapshot, ticket, snapshotErr := s.readSnapshot(ctx, p, adapter, gen)
			if snapshotErr == nil && snapshot.ServerInstanceID != nil && *snapshot.ServerInstanceID == instanceID {
				s.mu.Lock()
				if s.currentLocked(gen, p.ID) && s.eventSerial == serial && s.canCommitSnapshotLocked(ticket, snapshot) {
					cursor, snapshotErr = s.ensureEventScopeLocked(p, snapshot, true)
					if snapshotErr == nil {
						snapshot.ProfileID = p.ID
						s.applySnapshotLocked(p, snapshot, ticket)
						s.state.EventStatus = "connecting"
					}
				} else {
					snapshotErr = context.Canceled
				}
				s.mu.Unlock()
				s.publish()
				if snapshotErr == nil {
					failures = 0
					continue
				}
			}
		}
		failures++
		delay := time.Second << min(failures-1, 5)
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// acceptEventLocked 同一次提交推进游标、去重、回执和通知尝试，落盘失败时不发布业务提示。
func (s *Service) acceptEventLocked(p profile.Profile, event autobgi.Event) (bool, error) {
	next := cloneJournal(s.journal)
	index := eventScopeIndex(&next, p, event.InstanceID)
	if index < 0 {
		return false, errors.New("事件范围与本地同步基线不一致")
	}
	scope := &next.Scopes[index]
	order, err := autobgi.CompareEventCursors(scope.Cursor, event.EventID)
	if err != nil {
		return false, err
	}
	if order <= 0 {
		return false, nil
	}
	if containsID(scope.Seen, event.EventID) {
		return false, nil
	}
	scope.Cursor = event.EventID
	scope.Seen = appendBounded(scope.Seen, event.EventID)
	var delivery *Notification
	if event.Type == "command.updated" {
		var receipt autobgi.CommandReceipt
		if json.Unmarshal(event.Data, &receipt) != nil {
			return false, errors.New("命令事件格式无效")
		}
		// 其他客户端产生的命令不进入本桌宠的本地命令记录，也不触发本地完成提示。
		for _, known := range next.Commands {
			if known.BaseURL != p.BaseURL || known.Receipt.ProfileID != p.ID || known.Receipt.InstanceID != event.InstanceID || known.Receipt.CommandID != receipt.CommandID {
				continue
			}
			// 已提交终态不可被矛盾事件改写，也不能为被忽略的状态生成相反提示；事件游标仍正常提交。
			if terminalCommand(known.Receipt.Status) && receipt.Status != known.Receipt.Status {
				break
			}
			receipt.ProfileID, receipt.InstanceID, receipt.PresetName = p.ID, event.InstanceID, known.Receipt.PresetName
			if err := putCommand(&next, p.BaseURL, receipt); err != nil {
				return false, err
			}
			attemptKey := receipt.CommandID + ":" + receipt.Status
			occurredAt, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
			fresh := err == nil && time.Since(occurredAt) <= 5*time.Minute && time.Until(occurredAt) < time.Minute
			if terminalCommand(receipt.Status) && !containsID(scope.Attempted, attemptKey) {
				scope.Attempted = appendBounded(scope.Attempted, attemptKey)
				var stageErr error
				delivery, stageErr = s.stageCommandNotificationLocked(&next, p, receipt, event.OccurredAt, fresh)
				if stageErr != nil {
					return false, stageErr
				}
			}
			break
		}
	}
	if err := s.commitJournalLocked(next); err != nil {
		return false, err
	}
	if delivery != nil {
		s.enqueueCommittedNoticesLocked(*delivery)
	}
	return true, nil
}

// applySnapshotLocked 统一处理快照身份、确认失效和缓存，轮询与事件重新同步遵循同样边界。
func (s *Service) applySnapshotLocked(p profile.Profile, snapshot autobgi.Snapshot, ticket snapshotTicket) {
	// 游标重同步和轮询都必须携带有效采样票据，不能只凭相同 generation 回写。
	if !s.canCommitSnapshotLocked(ticket, snapshot) {
		return
	}
	snapshot.ProfileID = p.ID
	snapshot.Stale = false
	if prepared := s.state.PreparedCommand; prepared != nil {
		if snapshot.ServerInstanceID == nil || *snapshot.ServerInstanceID != prepared.InstanceID || snapshot.StateVersion == nil || *snapshot.StateVersion != prepared.ExpectedStateVersion || snapshot.AccountID != nil {
			s.state.PreparedCommand = nil
		}
	}
	s.state.Snapshot = &snapshot
	if s.cache.Put(p.BaseURL, snapshot) == nil {
		s.cacheDirty = true
		s.flushCacheLocked(false)
	}
}
