package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
)

// logLoop 是唯一的日志采集循环，旧连接、休眠和网络中断都会取消当前订阅。
func (s *Service) logLoop(ctx context.Context, p profile.Profile, client *autobgi.Client, refresh <-chan struct{}, gen uint64) {
	defer s.wg.Done()
	defer client.Close()
	failures := 0
	for ctx.Err() == nil {
		s.mu.Lock()
		valid, paused := s.currentLocked(gen, p.ID), s.paused
		s.mu.Unlock()
		if !valid {
			return
		}
		if paused {
			if !wait(ctx, refresh, 30*time.Second) {
				return
			}
			continue
		}

		// 每次重连先重新验证密钥并选择服务端返回的最新日志。
		requestCtx, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		if !s.currentLocked(gen, p.ID) || s.paused {
			s.mu.Unlock()
			cancel()
			continue
		}
		s.requestCancel = cancel
		s.state.Connection.Status = "connecting"
		s.state.LogStream.Status = "connecting"
		s.state.LogStream.Error = ""
		s.state.LogStream.LastActivityAt = ""
		s.mu.Unlock()
		s.publish()

		files, err := client.ListLogFiles(requestCtx)
		if err == nil && len(files) == 0 {
			err = &autobgi.APIError{Code: "LOG_UNAVAILABLE", Message: "未找到 BetterGI 日志，等待新日志出现"}
		}
		if err == nil {
			started := time.Now()
			err = s.watchCurrentLog(requestCtx, p, client, files[0], gen)
			if time.Since(started) >= 60*time.Second {
				failures = 0
			}
		}
		cancel()

		s.mu.Lock()
		if !s.currentLocked(gen, p.ID) {
			s.mu.Unlock()
			return
		}
		s.requestCancel = nil
		s.state.LogStream.LastActivityAt = ""
		if s.paused {
			s.state.LogStream.Status = "paused"
			s.mu.Unlock()
			s.publish()
			continue
		}
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		if errors.Is(err, errLogRotated) {
			// 日志轮换不属于故障，立即订阅最新文件。
			failures = 0
			s.mu.Unlock()
			continue
		}
		code, message := "NETWORK_ERROR", "日志连接已中断，正在重新连接"
		var apiErr *autobgi.APIError
		if errors.As(err, &apiErr) {
			code, message = apiErr.Code, apiErr.Message
		}
		s.state.Connection.ErrorCode = code
		s.state.Connection.ErrorMessage = message
		s.state.LogStream.Error = message
		if code == "AUTH_INVALID" {
			s.state.Connection.Status = "auth_failed"
			s.state.Connection.RetryAt = nil
			s.state.LogStream.Status = "auth_failed"
			s.mu.Unlock()
			s.publish()
			return
		}
		s.state.Connection.Status = "backoff"
		s.state.LogStream.Status = "backoff"
		steps := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
		index := failures
		if index >= len(steps) {
			index = len(steps) - 1
		}
		failures++
		delay := steps[index]
		retry := time.Now().Add(delay).Format(time.RFC3339Nano)
		s.state.Connection.RetryAt = &retry
		s.mu.Unlock()
		s.publish()
		if !wait(ctx, refresh, delay) {
			return
		}
	}
}

var errLogRotated = errors.New("日志文件已轮换")

// RecordDroppedTrigger 向设置页报告队列溢出，仅更新仍有效的连接代次。
//
//wails:ignore
func (s *Service) RecordDroppedTrigger(gen uint64) {
	s.mu.Lock()
	if s.closed || s.state.Connection.Generation != gen {
		s.mu.Unlock()
		return
	}
	s.state.LogStream.DroppedTriggers++
	s.mu.Unlock()
	s.publish()
}

// watchCurrentLog 保持单条 WebSocket，并定期检查服务端是否产生更新的日志文件。
func (s *Service) watchCurrentLog(ctx context.Context, p profile.Profile, client *autobgi.Client, name string, gen uint64) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.WatchLog(streamCtx, name, func() {
			s.mu.Lock()
			if !s.currentLocked(gen, p.ID) || s.paused || streamCtx.Err() != nil {
				s.mu.Unlock()
				return
			}
			s.state.Connection.Status = "online"
			s.state.Connection.ErrorCode, s.state.Connection.ErrorMessage = "", ""
			s.state.Connection.RetryAt = nil
			s.state.LogStream.Status = "online"
			s.state.LogStream.FileName = client.RedactLog(name)
			s.state.LogStream.Error = ""
			s.state.LogStream.LastActivityAt = time.Now().Format(time.RFC3339Nano)
			s.mu.Unlock()
			s.publish()
		}, func(line string) {
			s.acceptLogLine(streamCtx, p.ID, gen, line)
		})
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			<-done
			return ctx.Err()
		case err := <-done:
			return err
		case <-ticker.C:
			// 列表接口继续使用密钥，因此不会因公开 WebSocket 掩盖失效的认证。
			files, err := client.ListLogFiles(streamCtx)
			if err != nil {
				cancel()
				<-done
				return err
			}
			if len(files) == 0 {
				cancel()
				<-done
				return &autobgi.APIError{Code: "LOG_UNAVAILABLE", Message: "日志文件已不可用，等待新日志出现"}
			}
			if files[0] != name {
				cancel()
				<-done
				return errLogRotated
			}
		}
	}
}

// pickupPattern 兼容实际英文引号、中文引号以及混合引号，日志前缀不会影响匹配。
var pickupPattern = regexp.MustCompile(`交互或拾取[[:space:]　]*[：:][[:space:]　]*["“]([^"”\r\n]+)["”]`)

// acceptLogLine 记录有限长度的实时日志，并逐次向宿主投递每次命中。
func (s *Service) acceptLogLine(ctx context.Context, profileID string, gen uint64, line string) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	// 匹配完整日志，展示截断不能影响较长行末尾的关键词。
	matchLine := line
	runes := []rune(line)
	if len(runes) > 4096 {
		line = string(runes[:4096]) + "…"
	}
	s.mu.Lock()
	if !s.currentLocked(gen, profileID) || s.paused || ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	// 每条有效日志立即刷新饥饿计时，文件列表轮询和 WebSocket 控制帧不计入活动。
	now := time.Now().Format(time.RFC3339Nano)
	s.state.LogStream.LastActivityAt = now
	s.state.LogStream.Logs = append(s.state.LogStream.Logs, LogLine{Time: now, Text: line})
	if len(s.state.LogStream.Logs) > 200 {
		s.state.LogStream.Logs = append([]LogLine(nil), s.state.LogStream.Logs[len(s.state.LogStream.Logs)-200:]...)
	}
	var fired *LogTrigger
	if rule, keyword := matchKeyword(s.config.KeywordRules, matchLine); rule != nil {
		s.logTriggerSequence++
		// 留空台词时使用已脱敏且受长度限制的原始日志，避免截断影响关键词匹配。
		text := rule.Text
		if strings.TrimSpace(text) == "" {
			text = line
		}
		fired = &LogTrigger{ID: s.logTriggerSequence, Generation: gen, Action: rule.Action, Text: text, Keyword: keyword}
		s.state.LogStream.LastTrigger = fired
		// 代办与动画队列独立落盘，窗口隐藏或动作队列满时也不会漏掉命中。
		s.createTodoLocked(*rule, profileID, text, time.Now())
	}
	// 同一行可以包含多个拾取项目，并且不影响该行已有的关键词匹配。
	var pickups []LogPickup
	if s.config.Preferences.Notifications.PickupEnabled {
		for _, match := range pickupPattern.FindAllStringSubmatch(matchLine, -1) {
			name := strings.TrimSpace(match[1])
			if name == "" {
				continue
			}
			s.logPickupSequence++
			pickups = append(pickups, LogPickup{ID: s.logPickupSequence, Generation: gen, Name: name})
		}
	}
	emit := s.logTriggerEmit
	emitPickup := s.logPickupEmit
	s.mu.Unlock()
	// 先广播已经刷新的活动时间，避免饿晕画面盖住这一行触发的新动作。
	s.publish()
	// 每个命中单独投递，状态事件仅用于同步显示，不能作为可靠的动作队列。
	if fired != nil && emit != nil {
		emit(*fired)
	}
	if emitPickup != nil {
		for _, pickup := range pickups {
			emitPickup(pickup)
		}
	}
}
