package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
)

// currentLocked 是所有异步写入的共同边界，阻止旧连接污染当前窗口。
func (s *Service) currentLocked(gen uint64, id string) bool {
	return !s.closed && s.state.Connection.Generation == gen && s.state.Connection.ProfileID == id
}

// wait 让退出、恢复、手动刷新与定时轮询共用一个可取消等待点。
func wait(ctx context.Context, trigger <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-trigger:
		return true
	case <-timer.C:
		return true
	}
}

// poll 是实例内唯一状态采集器；普通请求不并发，失败保留旧快照。
func (s *Service) poll(ctx context.Context, p profile.Profile, client *autobgi.Client, trigger <-chan struct{}, gen uint64) {
	defer s.wg.Done()
	defer client.Close()
	var adapter autobgi.AutoBGIAdapter
	var caps autobgi.Capabilities
	failures := 0
	var stableSince, lastAttempt time.Time
	disconnectedNotice := false
	connectedOnce := false
	for {
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		valid := s.currentLocked(gen, p.ID)
		paused := s.paused
		s.mu.Unlock()
		if !valid {
			return
		}
		if paused {
			if !wait(ctx, trigger, 30*time.Second) {
				return
			}
			continue
		}
		// 刷新最多每秒一次，即使前端连续点击也不绕过单请求限制。
		if remaining := time.Second - time.Since(lastAttempt); remaining > 0 {
			timer := time.NewTimer(remaining)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		lastAttempt = time.Now()
		requestCtx, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		if !s.currentLocked(gen, p.ID) {
			s.mu.Unlock()
			cancel()
			return
		}
		// 限流等待期间也可能断网或休眠，开始请求前再次检查，避免把暂停状态写回 connecting。
		if s.paused {
			s.mu.Unlock()
			cancel()
			continue
		}
		s.requestCancel = cancel
		if s.state.Connection.Status != "online" {
			s.state.Connection.Status = "connecting"
			s.state.Connection.RetryAt = nil
		}
		s.mu.Unlock()
		s.publish()
		var snapshot autobgi.Snapshot
		var ticket snapshotTicket
		var err error
		if adapter == nil {
			adapter, caps, snapshot, ticket, err = s.probeSnapshot(requestCtx, p, client, gen)
		} else {
			snapshot, ticket, err = s.readSnapshot(requestCtx, p, adapter, gen)
		}
		cancel()
		s.mu.Lock()
		if !s.currentLocked(gen, p.ID) {
			s.mu.Unlock()
			return
		}
		s.requestCancel = nil
		paused = s.paused
		if paused {
			s.mu.Unlock()
			continue
		}
		if errors.Is(err, context.Canceled) {
			s.mu.Unlock()
			// 快速睡眠/唤醒可能已取消请求但恢复 paused=false；保留采集循环继续同步。
			if ctx.Err() != nil {
				return
			}
			continue
		}
		// 后续核验已经取得较新样本时，旧轮询的错误也不能把新状态改为过期或退避。
		if ticket.sequence != 0 && ticket.sequence < s.snapshotObserved {
			s.mu.Unlock()
			continue
		}
		if err == nil {
			if !s.canCommitSnapshotLocked(ticket, snapshot) {
				s.mu.Unlock()
				continue
			}
			snapshot.ProfileID = p.ID
			snapshot.Stale = false
			previous := s.state.Snapshot
			s.applySnapshotLocked(p, snapshot, ticket)
			s.state.Capabilities = &caps
			s.adapter = adapter
			s.startEventsLocked(ctx, p, adapter, caps, snapshot, gen)
			s.state.Connection.Status = "online"
			s.state.Connection.ErrorCode = ""
			s.state.Connection.ErrorMessage = ""
			s.state.Connection.RetryAt = nil
			// 重启或跨实例切换只在当前核验范围内恢复一条未读摘要，不重放历史正文。
			if err := s.reconcileNotificationPolicyLocked(); err != nil {
				s.state.Notice = "提醒摘要暂无法保存，历史和待汇总计数保持不变"
			}
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= 60*time.Second {
				failures = 0
			}
			// 连接恢复只更新事实状态，不再额外投递“已连接”成功气泡。
			if disconnectedNotice {
				disconnectedNotice = false
			}
			if connectedOnce && previous != nil && !previous.Stale && text(previous.Task.GroupName) != text(snapshot.Task.GroupName) {
				s.addNoticeLocked("progress", "当前配置组已变化："+text(snapshot.Task.GroupName))
			}
			connectedOnce = true
			interval := 15 * time.Second
			if s.panelVisible || hasProgress(snapshot) {
				interval = 5 * time.Second
			}
			s.mu.Unlock()
			s.publish()
			// 普通轮询和事件唤醒共用同一个串行材料采集器，不按窗口数增加请求。
			if snapshot.ServerInstanceID != nil {
				s.syncMaterialsReady(ctx, p, adapter, caps, *snapshot.ServerInstanceID, gen)
				s.syncTargetsMet(ctx, p, adapter, caps, *snapshot.ServerInstanceID, gen)
			}
			if !wait(ctx, trigger, interval) {
				return
			}
			continue
		}
		// 认证失败及格式不兼容不会无限重试，更不会降级匿名访问。
		stableSince = time.Time{}
		s.invalidateMaterialsLocked(false)
		s.invalidateTargetsLocked(false)
		if s.state.Snapshot != nil {
			s.state.Snapshot.Stale = true
		}
		s.state.PreparedCommand = nil
		code, message := "NETWORK_ERROR", "无法连接服务，请检查地址、网络或服务是否启动"
		var apiError *autobgi.APIError
		if errors.As(err, &apiError) {
			code, message = apiError.Code, apiError.Message
		}
		s.state.Connection.ErrorCode = code
		s.state.Connection.ErrorMessage = message
		stop := false
		switch code {
		case "AUTH_INVALID":
			s.state.Connection.Status = "auth_failed"
			stop = true
		case "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE", "INCOMPATIBLE_PROTOCOL", "REDIRECT_REJECTED", "NOT_FOUND":
			s.state.Connection.Status = "incompatible"
			stop = true
		default:
			s.state.Connection.Status = "backoff"
		}
		if !disconnectedNotice {
			s.addNoticeLocked("error", message)
			disconnectedNotice = true
		}
		if stop {
			if s.eventCancel != nil {
				s.eventCancel()
				s.eventCancel = nil
			}
			s.state.EventStatus = "disconnected"
			s.state.Connection.RetryAt = nil
			s.mu.Unlock()
			s.publish()
			return
		}
		// 退避为 1/2/4/8/16/30 秒并加入 20% 抖动；稳定 60 秒后才重置。
		steps := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
		index := failures
		if index >= len(steps) {
			index = len(steps) - 1
		}
		failures++
		delay := time.Duration(float64(steps[index]) * (0.8 + rand.Float64()*0.4))
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		retry := time.Now().Add(delay).Format(time.RFC3339Nano)
		s.state.Connection.RetryAt = &retry
		s.mu.Unlock()
		s.publish()
		if !wait(ctx, trigger, delay) {
			return
		}
	}
}

// hasProgress 只决定轮询速度，不赋予进程或展示文本任务状态语义。
func hasProgress(snapshot autobgi.Snapshot) bool {
	return snapshot.Task.State == "running" || strings.TrimSpace(text(snapshot.Task.ProgressText)) != "" || strings.TrimSpace(text(snapshot.Task.ScriptProgressText)) != ""
}

// text 将未知展示值转为空字符串，仅用于文案，不替代业务 null。
func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// addNoticeLocked 先持久化本地事实，再交给实时展示队列；失败不生成另一条递归提醒。
func (s *Service) addNoticeLocked(kind, message string) {
	identity, ok := s.notificationContextLocked()
	if !ok {
		return
	}
	next := cloneJournal(s.journal)
	notice, err := s.stageNotificationLocked(&next, identity, kind, message, "local:"+randomID(), time.Now().Format(time.RFC3339Nano), true)
	if err == nil {
		err = s.commitJournalLocked(next)
	}
	if err != nil {
		s.journalBlocked = true
		s.state.Notice = "提醒记录无法保存，已停止本次提醒投递，请检查本地目录权限"
		return
	}
	if notice != nil {
		s.enqueueCommittedNoticesLocked(*notice)
	}
}
