package service

import (
	"context"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
)

// snapshotTicket 将一次采样绑定到连接、暂停周期和读取顺序，不向前端暴露内部控制信息。
type snapshotTicket struct {
	profileID  string
	generation uint64
	epoch      uint64
	sequence   uint64
	instanceID *string
}

// invalidateSnapshotsLocked 同时取消门内请求与等待者，快速暂停恢复也不能接纳暂停前的样本。
func (s *Service) invalidateSnapshotsLocked() {
	s.snapshotEpoch++
	if s.snapshotCancel != nil {
		s.snapshotCancel()
	}
	s.snapshotContext, s.snapshotCancel = nil, nil
}

// snapshotContextCurrentLocked 只校验生命周期，旧连接和旧暂停周期不能重新进入采样门。
func (s *Service) snapshotContextCurrentLocked(ticket snapshotTicket) bool {
	return !s.paused && s.currentLocked(ticket.generation, ticket.profileID) && ticket.epoch == s.snapshotEpoch
}

// snapshotInstanceMatchesLocked 同时核对采样时和提交时的实例身份，不把空实例当通配符。
func (s *Service) snapshotInstanceMatchesLocked(ticket snapshotTicket, snapshot autobgi.Snapshot) bool {
	if ticket.instanceID != nil && (snapshot.ServerInstanceID == nil || *snapshot.ServerInstanceID != *ticket.instanceID) {
		return false
	}
	if caps := s.state.Capabilities; caps != nil {
		if caps.SourceMode != snapshot.SourceMode || (caps.InstanceID == nil) != (snapshot.ServerInstanceID == nil) {
			return false
		}
		if caps.InstanceID != nil && *caps.InstanceID != *snapshot.ServerInstanceID {
			return false
		}
	}
	return true
}

// canCommitSnapshotLocked 拒绝同代迟到样本，较新采样即使尚未发布也不能被旧确认或旧游标覆盖。
func (s *Service) canCommitSnapshotLocked(ticket snapshotTicket, snapshot autobgi.Snapshot) bool {
	return ticket.sequence != 0 && ticket.sequence == s.snapshotObserved && s.snapshotContextCurrentLocked(ticket) && s.snapshotInstanceMatchesLocked(ticket, snapshot)
}

// acquireSnapshotGate 为所有状态来源提供可取消的串行门，等待与网络期间均不持业务锁。
func (s *Service) acquireSnapshotGate(ctx context.Context, p profile.Profile, generation uint64) (context.Context, snapshotTicket, func(), error) {
	s.mu.Lock()
	ticket := snapshotTicket{profileID: p.ID, generation: generation, epoch: s.snapshotEpoch}
	if !s.snapshotContextCurrentLocked(ticket) || s.connectionCtx == nil {
		s.mu.Unlock()
		return nil, ticket, nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, ticket, nil, err
	}
	if s.snapshotGate == nil {
		s.snapshotGate = make(chan struct{}, 1)
	}
	if s.snapshotContext == nil {
		s.snapshotContext, s.snapshotCancel = context.WithCancel(s.connectionCtx)
	}
	if caps := s.state.Capabilities; caps != nil && caps.InstanceID != nil {
		instanceID := *caps.InstanceID
		ticket.instanceID = &instanceID
	}
	gate, epochContext := s.snapshotGate, s.snapshotContext
	s.mu.Unlock()

	// 调用者超时、连接切换和暂停任一发生，都能立即中断排队及当前 HTTP 请求。
	requestContext, cancel := context.WithCancel(ctx)
	stopInterrupt := context.AfterFunc(epochContext, cancel)
	select {
	case <-requestContext.Done():
		cancel()
		stopInterrupt()
		return nil, ticket, nil, requestContext.Err()
	case gate <- struct{}{}:
	}
	release := func() { cancel(); stopInterrupt(); <-gate }

	s.mu.Lock()
	if !s.snapshotContextCurrentLocked(ticket) {
		s.mu.Unlock()
		release()
		return nil, ticket, nil, context.Canceled
	}
	if err := requestContext.Err(); err != nil {
		s.mu.Unlock()
		release()
		return nil, ticket, nil, err
	}
	s.snapshotSequence++
	ticket.sequence = s.snapshotSequence
	s.mu.Unlock()
	return requestContext, ticket, release, nil
}

// finishSnapshot 在释放串行门前登记最新有效样本，迟到消费者只能放弃旧序号。
func (s *Service) finishSnapshot(ctx context.Context, ticket snapshotTicket, snapshot autobgi.Snapshot, err error) (autobgi.Snapshot, snapshotTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snapshotContextCurrentLocked(ticket) {
		return autobgi.Snapshot{}, ticket, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return autobgi.Snapshot{}, ticket, err
	}
	if err != nil {
		return autobgi.Snapshot{}, ticket, err
	}
	if !s.snapshotInstanceMatchesLocked(ticket, snapshot) {
		return autobgi.Snapshot{}, ticket, &autobgi.APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "快照来源与当前连接实例不匹配"}
	}
	s.snapshotObserved = ticket.sequence
	return snapshot, ticket, nil
}

// readSnapshot 使命令核验、普通轮询和事件重同步使用相同的身份、顺序与取消边界。
func (s *Service) readSnapshot(ctx context.Context, p profile.Profile, adapter autobgi.AutoBGIAdapter, generation uint64) (autobgi.Snapshot, snapshotTicket, error) {
	requestContext, ticket, release, err := s.acquireSnapshotGate(ctx, p, generation)
	if err != nil {
		return autobgi.Snapshot{}, ticket, err
	}
	defer release()
	snapshot, err := adapter.GetSnapshot(requestContext, autobgi.SnapshotQuery{})
	return s.finishSnapshot(requestContext, ticket, snapshot, err)
}

// probeSnapshot 将探测和首个样本视为同次采样，Legacy 探测自带的 index 请求也必须串行并随暂停失效。
func (s *Service) probeSnapshot(ctx context.Context, p profile.Profile, client *autobgi.Client, generation uint64) (autobgi.AutoBGIAdapter, autobgi.Capabilities, autobgi.Snapshot, snapshotTicket, error) {
	requestContext, ticket, release, err := s.acquireSnapshotGate(ctx, p, generation)
	if err != nil {
		return nil, autobgi.Capabilities{}, autobgi.Snapshot{}, ticket, err
	}
	defer release()
	adapter, caps, err := autobgi.NewAdapter(requestContext, client, p.AdapterMode)
	var snapshot autobgi.Snapshot
	if err == nil {
		snapshot, err = adapter.GetSnapshot(requestContext, autobgi.SnapshotQuery{})
	}
	snapshot, ticket, err = s.finishSnapshot(requestContext, ticket, snapshot, err)
	if err != nil {
		return nil, autobgi.Capabilities{}, autobgi.Snapshot{}, ticket, err
	}
	return adapter, caps, snapshot, ticket, nil
}
