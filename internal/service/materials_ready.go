package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// MaterialsReadyState 仅展示已持久接收的材料事实，不改变任务状态或当前培养队首。
type MaterialsReadyState struct {
	ProfileID        string                       `json:"profileId"`
	InstanceID       string                       `json:"instanceId"`
	ExecutionContext string                       `json:"executionContext"`
	Generation       string                       `json:"generation"`
	Cursor           string                       `json:"cursor"`
	Status           string                       `json:"status"`
	Message          string                       `json:"message"`
	Stale            bool                         `json:"stale"`
	Truncated        bool                         `json:"truncated"`
	Events           []autobgi.MaterialReadyEvent `json:"events"`
}

// materialScopeIndex 空执行范围只寻找最近使用的来源，用旧水位探测时仍由服务端校验范围。
func materialScopeIndex(j *storage.Journal, p profile.Profile, instanceID, executionContext string) int {
	for index := len(j.MaterialScopes) - 1; index >= 0; index-- {
		scope := j.MaterialScopes[index]
		if scope.ProfileID == p.ID && scope.BaseURL == p.BaseURL && scope.InstanceID == instanceID &&
			(executionContext == "" || scope.ExecutionContext == executionContext) {
			return index
		}
	}
	return -1
}

// invalidateMaterialsLocked 作废已发出但未接纳的材料请求，恢复后的缺口统一只补历史。
func (s *Service) invalidateMaterialsLocked(clear bool) {
	s.materialSerial++
	s.materialHistoryOnly = true
	s.materialCollecting = false
	if s.materialCancel != nil {
		s.materialCancel()
		s.materialCancel = nil
	}
	if clear {
		s.state.MaterialsReady = nil
	} else if s.state.MaterialsReady != nil {
		s.state.MaterialsReady.Stale = true
		s.state.MaterialsReady.Status = "resyncing"
	}
}

// materialsCurrentLocked 同时限制连接代次、请求串号、休眠和已验证的服务实例。
func (s *Service) materialsCurrentLocked(p profile.Profile, instanceID string, gen, serial uint64) bool {
	return s.currentLocked(gen, p.ID) && !s.paused && s.materialSerial == serial &&
		s.state.Connection.Status == "online" && s.state.Capabilities != nil &&
		s.state.Capabilities.MaterialsReadyEvents && s.state.Capabilities.InstanceID != nil &&
		*s.state.Capabilities.InstanceID == instanceID
}

// materialFailureLocked 保留安全历史并标记过期，失败恢复不能把遗漏事件变成新提醒。
func (s *Service) materialFailureLocked(status, message string) {
	s.materialHistoryOnly = true
	if s.state.MaterialsReady == nil {
		s.state.MaterialsReady = &MaterialsReadyState{Events: []autobgi.MaterialReadyEvent{}}
	}
	s.state.MaterialsReady.Status = status
	s.state.MaterialsReady.Message = message
	s.state.MaterialsReady.Stale = true
}

// syncMaterialsReady 只由唯一轮询循环调用，事件唤醒合并到该循环，不创建额外采集协程。
func (s *Service) syncMaterialsReady(parent context.Context, p profile.Profile, adapter autobgi.AutoBGIAdapter, caps autobgi.Capabilities, instanceID string, gen uint64) {
	s.mu.Lock()
	if !s.currentLocked(gen, p.ID) || s.paused || s.state.Connection.Status != "online" {
		s.mu.Unlock()
		return
	}
	if !caps.MaterialsReadyEvents {
		s.state.MaterialsReady = nil
		s.mu.Unlock()
		return
	}
	reader, ok := adapter.(autobgi.MaterialsReadyReader)
	if !ok {
		s.materialFailureLocked("incompatible", "当前适配器不能安全读取材料历史")
		s.mu.Unlock()
		s.publish()
		return
	}
	if s.materialCollecting || s.journalBlocked {
		if s.journalBlocked {
			s.materialFailureLocked("persistence_failed", "本地记录不可用，已停止材料历史接收与提示")
		}
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.materialSerial++
	serial := s.materialSerial
	s.materialCancel, s.materialCollecting = cancel, true
	historyOnly := s.materialHistoryOnly
	var query *autobgi.MaterialsReadyQuery
	if index := materialScopeIndex(&s.journal, p, instanceID, ""); index >= 0 {
		scope := s.journal.MaterialScopes[index]
		query = &autobgi.MaterialsReadyQuery{After: scope.Cursor, Generation: scope.Generation, ExecutionContext: scope.ExecutionContext}
	} else {
		historyOnly = true
	}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if s.materialSerial == serial {
			s.materialCancel, s.materialCollecting = nil, false
		}
		s.mu.Unlock()
	}()
	// 每轮最多接收十页，超大恢复缺口留到下次轮询，期间所有页面保持只补历史。
	resynced := false
	for pageIndex := 0; pageIndex < 10 && ctx.Err() == nil; pageIndex++ {
		page, err := reader.ReadMaterialsReady(ctx, query)
		if err == nil {
			err = autobgi.ValidateMaterialsReadyPage(page, instanceID, query)
		}
		s.mu.Lock()
		if ctx.Err() != nil || !s.materialsCurrentLocked(p, instanceID, gen, serial) {
			s.mu.Unlock()
			return
		}
		if err != nil {
			var apiErr *autobgi.APIError
			if errors.As(err, &apiErr) && apiErr.Code == "RESYNC_REQUIRED" && query != nil && !resynced {
				// 新代次改读原子历史基线，保留稳定业务去重记录，不重放气泡。
				query, historyOnly, resynced = nil, true, true
				s.materialHistoryOnly = true
				if s.state.MaterialsReady != nil {
					s.state.MaterialsReady.Stale = true
					s.state.MaterialsReady.Status = "resyncing"
					s.state.MaterialsReady.Message = "材料记录范围已变化，正在重新建立历史基线"
				}
				s.mu.Unlock()
				s.publish()
				continue
			}
			status, message := "backoff", "材料历史暂时不可用，恢复后只补历史"
			if errors.As(err, &apiErr) {
				switch apiErr.Code {
				case "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE", "NOT_FOUND", "INVALID_QUERY":
					status, message = "incompatible", "材料历史协议不兼容，未接收未经核实的记录"
				case "AUTH_INVALID":
					status, message = "auth_failed", "材料历史鉴权失败，请检查当前连接"
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
			s.materialFailureLocked(status, message)
			s.mu.Unlock()
			s.publish()
			return
		}
		// 无查询的首次页始终只补历史；续页保留本次恢复过程的抑制语义。
		err = s.acceptMaterialsReadyLocked(p, page, historyOnly || query == nil, time.Now())
		if err != nil {
			s.materialFailureLocked("persistence_failed", "材料记录未能安全保存，未展示新的材料提示")
			s.mu.Unlock()
			s.publish()
			return
		}
		if !page.HasMore {
			s.materialHistoryOnly = false
			s.mu.Unlock()
			s.publish()
			return
		}
		// 多页说明采集中存在积压，之后的页面不能被当作实时到达逐条提醒。
		historyOnly = true
		s.materialHistoryOnly = true
		query = &autobgi.MaterialsReadyQuery{After: page.Cursor, Generation: page.Generation, ExecutionContext: page.ExecutionContext}
		s.mu.Unlock()
		s.publish()
	}
}

// trimMaterialSeen 只淘汰已超出可展示时间的记录，新鲜窗口过载时拒绝推进水位。
func trimMaterialSeen(items []storage.MaterialSeen, now time.Time) ([]storage.MaterialSeen, bool) {
	for len(items) > storage.MaterialSeenLimit {
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

// materialMessage 在本地生成固定事实文案，角色与配置文本经过协议校验和脱敏。
func materialMessage(event autobgi.MaterialReadyEvent) string {
	if event.Kind == autobgi.MaterialQueueReady {
		return "「" + event.ConfigName + "」本次培养队列的材料条件均已满足，可以添加新目标；游戏内升级与天赋加点请自行确认"
	}
	return "「" + event.CharacterName + "」的培养材料条件已满足；游戏内升级与天赋加点请自行确认"
}

// materialNoticeID 将来源与业务身份绑定，通知中心不能把同一业务的重投递当作新提示。
func materialNoticeID(p profile.Profile, instanceID, businessID string) string {
	sum := sha256.Sum256([]byte(p.ID + "\x00" + p.BaseURL + "\x00" + instanceID + "\x00" + businessID))
	return "materials-ready:" + hex.EncodeToString(sum[:])
}

// applyMaterialHistoryLocked 只从已提交范围构造展示状态，空页同步无需反复写入同一份磁盘记录。
func (s *Service) applyMaterialHistoryLocked(p profile.Profile, scope storage.MaterialScope) {
	s.state.MaterialsReady = &MaterialsReadyState{ProfileID: p.ID, InstanceID: scope.InstanceID,
		ExecutionContext: scope.ExecutionContext, Generation: scope.Generation, Cursor: scope.Cursor, Status: "online",
		Truncated: scope.Truncated, Events: append([]autobgi.MaterialReadyEvent{}, scope.History...)}
}

// acceptMaterialsReadyLocked 一次提交水位、去重、历史与展示处理标记，成功之后才发布通知。
func (s *Service) acceptMaterialsReadyLocked(p profile.Profile, page autobgi.MaterialsReadyPage, historyOnly bool, now time.Time) error {
	if index := materialScopeIndex(&s.journal, p, page.InstanceID, page.ExecutionContext); index >= 0 && index == len(s.journal.MaterialScopes)-1 {
		scope := s.journal.MaterialScopes[index]
		if len(page.Events) == 0 && scope.Generation == page.Generation && scope.Cursor == page.Cursor && (!page.Truncated || scope.Truncated) {
			s.applyMaterialHistoryLocked(p, scope)
			return nil
		}
	}
	next := cloneJournal(s.journal)
	index := materialScopeIndex(&next, p, page.InstanceID, page.ExecutionContext)
	if index < 0 {
		if len(next.MaterialScopes) >= storage.MaterialScopeLimit {
			next.MaterialScopes = append([]storage.MaterialScope(nil), next.MaterialScopes[1:]...)
		}
		next.MaterialScopes = append(next.MaterialScopes, storage.MaterialScope{ProfileID: p.ID, BaseURL: p.BaseURL,
			InstanceID: page.InstanceID, ExecutionContext: page.ExecutionContext, History: []autobgi.MaterialReadyEvent{}, Seen: []storage.MaterialSeen{}})
		index = len(next.MaterialScopes) - 1
	} else if index != len(next.MaterialScopes)-1 {
		// 最近接收的执行范围放在末尾，切回旧目录后不能每次都探测另一目录的水位。
		value := next.MaterialScopes[index]
		next.MaterialScopes = append(next.MaterialScopes[:index], next.MaterialScopes[index+1:]...)
		next.MaterialScopes = append(next.MaterialScopes, value)
		index = len(next.MaterialScopes) - 1
	}
	scope := &next.MaterialScopes[index]
	seen := make(map[string]storage.MaterialSeen, len(scope.Seen))
	for _, item := range scope.Seen {
		seen[item.BusinessEventID] = item
	}
	history := make(map[string]autobgi.MaterialReadyEvent, len(scope.History))
	for _, event := range scope.History {
		history[event.BusinessEventID] = event
	}
	notices := []Notification{}
	newHistory := []autobgi.MaterialReadyEvent{}
	for _, event := range page.Events {
		fingerprint := storage.MaterialFingerprint(event)
		if known, ok := seen[event.BusinessEventID]; ok {
			if known.Fingerprint != fingerprint || known.OccurredAt != event.OccurredAt {
				return errors.New("同一材料业务身份对应矛盾事实")
			}
			continue
		}
		if known, ok := history[event.BusinessEventID]; ok {
			if storage.MaterialFingerprint(known) != fingerprint {
				return errors.New("材料历史身份发生冲突")
			}
			continue
		}
		// 历史补偿与过期事件也记作已处理，之后换传输代次不能补弹。
		item := storage.MaterialSeen{BusinessEventID: event.BusinessEventID, Fingerprint: fingerprint,
			OccurredAt: event.OccurredAt, PresentationHandled: true}
		scope.Seen = append(scope.Seen, item)
		seen[event.BusinessEventID] = item
		scope.History = append(scope.History, event)
		history[event.BusinessEventID] = event
		newHistory = append(newHistory, event)
	}
	var valid bool
	scope.Seen, valid = trimMaterialSeen(scope.Seen, now)
	if !valid {
		return errors.New("材料提示去重窗口已满，停止接收以避免重复提示")
	}
	if len(scope.History) > storage.MaterialHistoryLimit {
		scope.History = append([]autobgi.MaterialReadyEvent(nil), scope.History[len(scope.History)-storage.MaterialHistoryLimit:]...)
		scope.Truncated = true
	}
	scope.Generation, scope.Cursor = page.Generation, page.Cursor
	scope.Truncated = scope.Truncated || page.Truncated
	// 材料事实、接收水位与普通提醒正文共用同次 journal 提交，历史补偿只入箱不补弹。
	identity := storage.NotificationScope{ProfileID: p.ID, BaseURL: p.BaseURL, SourceMode: "pet-v1", InstanceID: page.InstanceID}
	for _, event := range newHistory {
		occurred, _ := time.Parse(time.RFC3339Nano, event.OccurredAt)
		live := !historyOnly && !page.HasMore && now.Sub(occurred) <= 5*time.Minute && occurred.Sub(now) < time.Minute
		notice, err := s.stageNotificationLocked(&next, identity, "materials", materialMessage(event), "material:"+event.BusinessEventID, event.OccurredAt, live)
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
	s.applyMaterialHistoryLocked(p, s.journal.MaterialScopes[index])
	// 同批提醒超出展示容量时保留摘要，完整事实仍可在材料历史中查看。
	if len(notices) > 20 {
		last := notices[len(notices)-1]
		summary := Notification{ID: last.ID + ":summary", Kind: "materials", CreatedAt: last.CreatedAt,
			Message: fmt.Sprintf("另有 %d 条材料条件提醒已合并，请在材料条件记录中查看；游戏内升级与天赋加点请自行确认", len(notices)-19)}
		notices = append(append([]Notification(nil), notices[len(notices)-19:]...), summary)
	}
	// 接收时间不替代业务发生时间，重启也不重新把持久历史灌入通知队列。
	s.enqueueCommittedNoticesLocked(notices...)
	return nil
}
