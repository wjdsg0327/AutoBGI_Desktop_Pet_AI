package service

import (
	"context"
	"errors"
	"reflect"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
)

// manualReadTicket 冻结手动只读请求的完整来源，暂停恢复、能力变化和适配器替换均不能接纳旧结果。
type manualReadTicket struct {
	profile      profile.Profile
	generation   uint64
	epoch        uint64
	client       *autobgi.Client
	adapter      autobgi.AutoBGIAdapter
	capabilities autobgi.Capabilities
}

// cloneManualCapabilities 深复制可变字段，原地修改实例、命令列表或保留策略也能被提交检查发现。
func cloneManualCapabilities(caps autobgi.Capabilities) autobgi.Capabilities {
	if caps.InstanceID != nil {
		instanceID := *caps.InstanceID
		caps.InstanceID = &instanceID
	}
	if caps.Commands != nil {
		caps.Commands = append([]string{}, caps.Commands...)
	}
	if caps.CommandPolicy != nil {
		policy := *caps.CommandPolicy
		caps.CommandPolicy = &policy
	}
	return caps
}

// sameManualAdapter 对非可比较的动态实现安全拒绝，避免接口相等运算在业务入口触发 panic。
func sameManualAdapter(left, right autobgi.AutoBGIAdapter) bool {
	adapterType := reflect.TypeOf(left)
	return adapterType != nil && adapterType == reflect.TypeOf(right) && adapterType.Comparable() && left == right
}

// beginManualReadLocked 在持锁时创建可被暂停立即取消的读取；调用者解锁后必须执行 release。
func (s *Service) beginManualReadLocked(timeout time.Duration) (manualReadTicket, context.Context, func(), error) {
	ticket := manualReadTicket{}
	p, valid := s.activeProfileLocked()
	if !valid || s.closed || s.paused || s.state.Connection.Status != "online" || s.adapter == nil ||
		s.client == nil || s.connectionCtx == nil || s.connectionCtx.Err() != nil || s.state.Capabilities == nil ||
		!sameManualAdapter(s.adapter, s.adapter) {
		return ticket, nil, nil, errors.New("当前连接已失效，请重新连接后读取")
	}
	if p.PreferredAccountID != nil {
		accountID := *p.PreferredAccountID
		p.PreferredAccountID = &accountID
	}
	ticket = manualReadTicket{
		profile: p, generation: s.state.Connection.Generation, epoch: s.snapshotEpoch,
		client: s.client, adapter: s.adapter, capabilities: cloneManualCapabilities(*s.state.Capabilities),
	}
	// 与快照复用暂停周期取消源，停止长连接之外的睡眠、断网也能中断手动查询。
	if s.snapshotContext == nil {
		s.snapshotContext, s.snapshotCancel = context.WithCancel(s.connectionCtx)
	}
	if s.snapshotContext.Err() != nil {
		return manualReadTicket{}, nil, nil, errors.New("当前读取周期已失效，请重新同步后读取")
	}
	ctx, cancel := context.WithTimeout(s.snapshotContext, timeout)
	s.wg.Add(1)
	return ticket, ctx, func() { cancel(); s.wg.Done() }, nil
}

// manualReadCurrentLocked 在返回或持久化前复核冻结来源，即使读取器忽略取消也不能提交迟到结果。
func (s *Service) manualReadCurrentLocked(ticket manualReadTicket, ctx context.Context) bool {
	p, valid := s.activeProfileLocked()
	return valid && !s.paused && ctx.Err() == nil && s.connectionCtx != nil && s.connectionCtx.Err() == nil &&
		s.currentLocked(ticket.generation, ticket.profile.ID) && s.snapshotEpoch == ticket.epoch &&
		s.state.Connection.Status == "online" && s.client == ticket.client && sameManualAdapter(s.adapter, ticket.adapter) &&
		reflect.DeepEqual(p, ticket.profile) && s.state.Capabilities != nil && reflect.DeepEqual(*s.state.Capabilities, ticket.capabilities)
}
