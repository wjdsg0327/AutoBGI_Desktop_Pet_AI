package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
)

// OneLongStartError 表示原生菜单启动的一次结果，Unknown 表示请求可能已经到达服务端。
type OneLongStartError struct {
	Code    string
	Message string
	Unknown bool
}

// Error 将原生菜单启动状态转换为可直接展示的本地文案。
func (e *OneLongStartError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// oneLongMenuError 统一构造不会泄漏服务端原始响应的本地错误。
func oneLongMenuError(code, message string) error {
	return &OneLongStartError{Code: code, Message: message}
}

// oneLongRequestUnknown 将 POST 后的网络、超时和连接取消统一标记为结果未知，禁止调用方自动重试。
func oneLongRequestUnknown(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *autobgi.APIError
	if errors.As(err, &apiErr) {
		if apiErr.HTTPStatus >= 500 || apiErr.HTTPStatus == 408 {
			return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "启动请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
		}
		switch apiErr.Code {
		case "NETWORK_ERROR", "INVALID_RESPONSE", "INCOMPATIBLE_RESPONSE":
			return &OneLongStartError{Code: apiErr.Code, Message: "启动请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
		default:
			return &OneLongStartError{Code: apiErr.Code, Message: apiErr.Message}
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "启动请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
	}
	return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "启动请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
}

// ensureMenuAdapter 在日志连接已在线但尚未建立业务适配器时执行一次只读探测。
func (s *Service) ensureMenuAdapter() error {
	s.adapterInitMu.Lock()
	defer s.adapterInitMu.Unlock()

	s.mu.Lock()
	p, valid := s.activeProfileLocked()
	if !valid || s.closed || s.paused || s.state.Connection.Status != "online" ||
		s.client == nil || s.connectionCtx == nil || s.connectionCtx.Err() != nil {
		s.mu.Unlock()
		return errors.New("当前连接尚未在线，请等待连接完成后重试")
	}
	if s.adapter != nil && s.state.Capabilities != nil {
		s.mu.Unlock()
		return nil
	}
	client, parent, generation, epoch := s.client, s.connectionCtx, s.state.Connection.Generation, s.snapshotEpoch
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()

	// 探测复用快照门和暂停周期，切换连接、休眠或断网后旧探测会被取消。
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	adapter, capabilities, _, _, err := s.probeSnapshot(ctx, p, client, generation)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if !s.currentLocked(generation, p.ID) || s.paused || s.state.Connection.Status != "online" ||
		s.client != client || s.connectionCtx != parent || s.snapshotEpoch != epoch || ctx.Err() != nil {
		s.mu.Unlock()
		return errors.New("连接已变化，请重新打开菜单后重试")
	}
	if s.adapter != nil && s.state.Capabilities != nil {
		s.mu.Unlock()
		return nil
	}
	s.adapter = adapter
	s.state.Capabilities = &capabilities
	s.mu.Unlock()
	// 探测结果进入当前连接状态后立即广播，等待中的菜单刷新可以看到新能力。
	s.publish()
	return nil
}

// menuConnectionMatchesLocked 核对原生菜单创建时冻结的连接、活动资料和暂停周期。
func (s *Service) menuConnectionMatchesLocked(profileID, activeProfileID string, generation uint64) bool {
	return !s.closed && !s.paused && s.state.Connection.Status == "online" &&
		s.state.Connection.ProfileID == profileID &&
		s.state.ActiveProfileID == activeProfileID &&
		s.state.Connection.Generation == generation
}

// setMenuStarting 让一条龙与配置组的重复点击立即失败，不等待第一次旧接口请求返回。
func (s *Service) setMenuStarting(starting bool) bool {
	s.menuStartMu.Lock()
	defer s.menuStartMu.Unlock()
	if starting {
		if s.menuStarting {
			return false
		}
		s.menuStarting = true
		return true
	}
	s.menuStarting = false
	return true
}

// StartOneLongFromMenu 只供原生菜单调用，读取当前配置和进程状态后提交一次固定旧接口请求。
//
//wails:ignore
func (s *Service) StartOneLongFromMenu(name string) error {
	s.mu.Lock()
	profileID := s.state.Connection.ProfileID
	activeProfileID := s.state.ActiveProfileID
	generation := s.state.Connection.Generation
	s.mu.Unlock()
	return s.StartOneLongFromMenuForConnection(name, profileID, activeProfileID, generation)
}

// StartOneLongFromMenuForConnection 使用菜单创建时冻结的连接身份，防止切换后把同名配置发送到新连接。
//
//wails:ignore
func (s *Service) StartOneLongFromMenuForConnection(name, expectedProfileID, expectedActiveProfileID string, expectedGeneration uint64) error {
	return s.startOneLongForConnection(name, expectedProfileID, expectedActiveProfileID, expectedGeneration, false)
}

// startOneLongForConnection 区分原菜单保护与用户点击代办时明确授权的切换。
func (s *Service) startOneLongForConnection(name, expectedProfileID, expectedActiveProfileID string, expectedGeneration uint64, allowRunning bool) error {
	target := strings.TrimSpace(name)
	if target == "" {
		return oneLongMenuError("INVALID_ARGUMENT", "一条龙名称不能为空")
	}
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(expectedProfileID, expectedActiveProfileID, expectedGeneration) {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}
	s.mu.Unlock()
	if !s.setMenuStarting(true) {
		return oneLongMenuError("TASK_BUSY", "已有启动请求正在处理，请稍后查看桌宠提示")
	}
	defer s.setMenuStarting(false)

	if err := s.ensureMenuAdapter(); err != nil {
		return oneLongMenuError("CONNECTION_UNAVAILABLE", err.Error())
	}
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(expectedProfileID, expectedActiveProfileID, expectedGeneration) {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(20 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_UNAVAILABLE", err.Error())
	}
	starter, ok := ticket.adapter.(autobgi.OneLongStarter)
	s.mu.Unlock()
	if !ok {
		release()
		return oneLongMenuError("UNSUPPORTED_CAPABILITY", "当前连接未提供一条龙启动能力")
	}
	defer release()

	// 先从当前连接重新读取列表，避免旧菜单项在服务端删除后仍被发送。
	presets, err := ticket.adapter.ListPresets(ctx, "")
	if err != nil {
		return oneLongMenuError("LIST_FAILED", err.Error())
	}
	selected := ""
	for _, preset := range presets {
		if strings.TrimSpace(preset.Name) == target {
			selected = strings.TrimSpace(preset.Name)
			break
		}
	}
	if selected == "" {
		return oneLongMenuError("PRESET_NOT_FOUND", "所选一条龙配置已失效，请重新打开菜单")
	}

	// 发送前重新读取进程快照，状态未知或已经运行都不能从菜单覆盖当前任务。
	snapshot, _, err := s.readSnapshot(ctx, ticket.profile, ticket.adapter, ticket.generation)
	if err != nil {
		return oneLongMenuError("STATUS_UNAVAILABLE", "无法核实 BGI 当前状态，已拒绝启动")
	}
	if snapshot.Process.Running == nil {
		return oneLongMenuError("STATUS_UNKNOWN", "BGI 当前状态未知，已拒绝启动")
	}
	if *snapshot.Process.Running && !allowRunning {
		return oneLongMenuError("TASK_BUSY", "检测到 BGI 正在运行，已拒绝启动")
	}

	// 列表和快照完成后再次核对来源，连接切换期间绝不把旧目标发送到新连接。
	s.mu.Lock()
	current := s.manualReadCurrentLocked(ticket, ctx)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}

	err = starter.StartOneLong(ctx, selected)
	if err != nil {
		return oneLongRequestUnknown(err)
	}

	// 只有当前连接仍然有效时才唤醒状态采集，避免旧连接结果污染新 profile。
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx)
	s.mu.Unlock()
	if !current {
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "连接已变化，启动请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
	}
	s.Refresh()
	return nil
}
