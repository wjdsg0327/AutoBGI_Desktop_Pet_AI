package service

import (
	"net"
	"net/url"
	"strings"
)

// SetNetworkAvailable 仅接收本机系统网络状态，不根据某次 AutoBGI 请求失败推断设备断网。
//
//wails:ignore
func (s *Service) SetNetworkAvailable(available bool) {
	s.mu.Lock()
	if s.closed || s.networkOffline == !available {
		s.mu.Unlock()
		return
	}
	s.networkOffline = !available
	s.reconcilePauseLocked()
	s.mu.Unlock()
	s.Refresh()
	s.publish()
}

// loopbackAddress 只识别字面回环地址与 localhost，不为判断豁免而发送 DNS 请求。
func loopbackAddress(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// needsNetworkLocked 保留当前连接的实际地址语义，未连接时仍保留断网事实供后续 Connect 使用。
func (s *Service) needsNetworkLocked() bool {
	p, ok := s.activeProfileLocked()
	return !ok || !loopbackAddress(p.BaseURL)
}

// pauseStatusMutableLocked 只改变仍在工作的连接，不借网络恢复重新启动认证失败或不兼容的采集器。
func (s *Service) pauseStatusMutableLocked() bool {
	if s.connectionCtx == nil || s.connectionCtx.Err() != nil {
		return false
	}
	switch s.state.Connection.Status {
	case "auth_failed", "incompatible", "disconnected":
		return false
	default:
		return true
	}
}

// reconcilePauseLocked 合并两个独立原因；最后一个原因消失后也必须先完整同步，不能直接恢复在线状态。
func (s *Service) reconcilePauseLocked() {
	wasPaused := s.paused
	networkPaused := s.networkOffline && s.needsNetworkLocked()
	s.paused = s.sleepPaused || networkPaused
	if s.paused && !wasPaused {
		// 暂停时不沿用旧订阅的日志活动时间，恢复后由新连接重新计时。
		s.state.LogStream.LastActivityAt = ""
		s.invalidateMaterialsLocked(false)
		s.invalidateTargetsLocked(false)
		s.invalidateSnapshotsLocked()
		s.state.PreparedCommand = nil
		// 立即废弃旧流，包括已经解析、等待业务锁的回调和失败返回分支。
		s.eventSerial++
		if s.eventCancel != nil {
			s.eventCancel()
			s.eventCancel = nil
		}
		s.eventStarted = false
		if s.requestCancel != nil {
			s.requestCancel()
		}
	}
	if !s.pauseStatusMutableLocked() {
		return
	}
	if s.paused {
		s.state.Connection.Status = "paused"
		s.state.LogStream.Status = "paused"
		s.state.Connection.RetryAt = nil
		s.state.EventStatus = "paused"
		if s.state.Snapshot != nil {
			s.state.Snapshot.Stale = true
		}
		switch {
		case networkPaused && s.sleepPaused:
			s.state.Connection.ErrorCode = "NETWORK_OFFLINE"
			s.state.Connection.ErrorMessage = "系统休眠且本机网络不可用，已暂停同步；两项恢复后将重新读取完整状态"
		case networkPaused:
			s.state.Connection.ErrorCode = "NETWORK_OFFLINE"
			s.state.Connection.ErrorMessage = "本机没有可用网络接口，已暂停同步；网络恢复后将重新读取完整状态"
		default:
			s.state.Connection.ErrorCode = "SYSTEM_SLEEP"
			s.state.Connection.ErrorMessage = "系统休眠，已暂停同步；唤醒后将重新读取完整状态"
		}
	} else if wasPaused {
		s.state.Connection.Status = "connecting"
		s.state.LogStream.Status = "connecting"
		s.state.Connection.ErrorCode, s.state.Connection.ErrorMessage = "", ""
		s.state.Connection.RetryAt = nil
		s.state.EventStatus = "resyncing"
		// 快照继续标记过期，只有 poll 接纳新采样后才能恢复在线和订阅事件。
		if s.state.Snapshot != nil {
			s.state.Snapshot.Stale = true
		}
	}
}
