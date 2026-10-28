package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// activeProfileLocked 只接受当前实际连接，不能从默认选择跨实例推断控制对象。
func (s *Service) activeProfileLocked() (profile.Profile, bool) {
	for _, p := range s.config.Profiles {
		if p.ID == s.state.Connection.ProfileID {
			return p, true
		}
	}
	return profile.Profile{}, false
}

// commandsLocked 按连接地址和探测到的实例过滤历史，切换连接时不混入其他实例回执。
func (s *Service) commandsLocked() []autobgi.CommandReceipt {
	result := []autobgi.CommandReceipt{}
	p, ok := s.activeProfileLocked()
	if !ok || s.state.Capabilities == nil || s.state.Capabilities.InstanceID == nil {
		return result
	}
	for i := len(s.journal.Commands) - 1; i >= 0; i-- {
		entry := s.journal.Commands[i]
		if entry.Receipt.ProfileID == p.ID && entry.BaseURL == p.BaseURL && entry.Receipt.InstanceID == *s.state.Capabilities.InstanceID {
			result = append(result, entry.Receipt)
		}
	}
	return result
}

// cloneJournal 通过值复制所有切片，落盘失败时不会提前污染内存中的已提交状态。
func cloneJournal(j storage.Journal) storage.Journal {
	data, _ := json.Marshal(j)
	var result storage.Journal
	_ = json.Unmarshal(data, &result)
	return result
}

// commitJournalLocked 所有写入统一先落盘再替换内存，失败后禁止发送新命令。
func (s *Service) commitJournalLocked(next storage.Journal) error {
	if s.journalBlocked {
		return errors.New("本地记录已停止写入，请修复目录或记录文件后重新启动桌宠")
	}
	if s.store == nil {
		s.journalBlocked = true
		return errors.New("命令记录存储未初始化，已停止控制操作")
	}
	if err := s.store.SaveJournal(next); err != nil {
		s.journalBlocked = true
		s.state.Notice = err.Error()
		return err
	}
	s.journal = next
	return nil
}

// terminalCommand 表示启动动作的已知终态，不表示整个游戏任务已经完成。
func terminalCommand(status string) bool {
	return status == "succeeded" || status == "failed" || status == "rejected"
}

// putCommand 保留较新的终态，避免 POST 的迟到 accepted 覆盖 SSE 已确认结果。
func putCommand(j *storage.Journal, baseURL string, receipt autobgi.CommandReceipt) error {
	for i := range j.Commands {
		if j.Commands[i].Receipt.CommandID == receipt.CommandID && j.Commands[i].Receipt.ProfileID == receipt.ProfileID && j.Commands[i].Receipt.InstanceID == receipt.InstanceID && j.Commands[i].BaseURL == baseURL {
			old := j.Commands[i].Receipt
			if terminalCommand(old.Status) && receipt.Status != old.Status {
				return nil
			}
			if old.Status == "executing" && receipt.Status == "accepted" {
				return nil
			}
			j.Commands[i].Receipt = receipt
			return nil
		}
	}
	// 只淘汰已知终态；结果未知的命令保留，以便用户使用原标识对账。
	if len(j.Commands) >= 200 {
		index := -1
		for i, entry := range j.Commands {
			if terminalCommand(entry.Receipt.Status) {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("未核实命令记录已达上限，请先查询历史回执")
		}
		j.Commands = append(j.Commands[:index], j.Commands[index+1:]...)
	}
	j.Commands = append(j.Commands, storage.CommandEntry{BaseURL: baseURL, Receipt: receipt})
	return nil
}

// removeJournalProfileLocked 删除连接时同步删除本地非敏感历史，其他连接不受影响。
func (s *Service) removeJournalProfileLocked(id string) {
	next := cloneJournal(s.journal)
	keptCommands := []storage.CommandEntry{}
	for _, entry := range next.Commands {
		if entry.Receipt.ProfileID != id {
			keptCommands = append(keptCommands, entry)
		}
	}
	keptScopes := []storage.EventScope{}
	for _, scope := range next.Scopes {
		if scope.ProfileID != id {
			keptScopes = append(keptScopes, scope)
		}
	}
	next.Commands, next.Scopes = keptCommands, keptScopes
	// 删除资料同时清理材料和普通通知范围，不能让同名重建资料继承旧历史。
	keptMaterials := []storage.MaterialScope{}
	for _, scope := range next.MaterialScopes {
		if scope.ProfileID != id {
			keptMaterials = append(keptMaterials, scope)
		}
	}
	keptNotifications := []storage.NotificationScope{}
	for _, scope := range next.NotificationScopes {
		if scope.ProfileID != id {
			keptNotifications = append(keptNotifications, scope)
		}
	}
	next.MaterialScopes, next.NotificationScopes = keptMaterials, keptNotifications
	// 目标达标历史有独立来源，删除资料时一并清理，不能被同名资料继承。
	keptTargets := []storage.TargetScope{}
	for _, scope := range next.TargetScopes {
		if scope.ProfileID != id {
			keptTargets = append(keptTargets, scope)
		}
	}
	next.TargetScopes = keptTargets
	_ = s.commitJournalLocked(next)
}

// commandContextLocked 同时核对用户控制开关和服务端幂等、对账能力，旧协议无法放行。
func (s *Service) commandContextLocked() (profile.Profile, autobgi.AutoBGIAdapter, context.Context, uint64, error) {
	p, ok := s.activeProfileLocked()
	caps := s.state.Capabilities
	if s.closed || s.paused || !ok || s.state.Connection.Status != "online" || s.adapter == nil || s.connectionCtx == nil {
		return p, nil, nil, 0, errors.New("请先连接并同步当前实例")
	}
	if p.ReadOnly {
		return p, nil, nil, 0, errors.New("当前连接为只读，请在连接设置中明确开启控制操作")
	}
	allowed := false
	if caps != nil {
		for _, kind := range caps.Commands {
			if kind == "oneLong.start" {
				allowed = true
			}
		}
	}
	if caps == nil || caps.SourceMode != "pet-v1" || caps.ExecutionScope != "instance" || !caps.CommandStatus || !caps.CommandIdempotency || caps.CommandPolicy == nil || !allowed {
		return p, nil, nil, 0, errors.New("当前服务没有完整的实例级启动、幂等或回执查询能力")
	}
	if s.journalBlocked {
		return p, nil, nil, 0, errors.New("本地命令记录未通过安全校验，请先核对 AutoBGI 历史命令和配置目录")
	}
	return p, s.adapter, s.connectionCtx, s.state.Connection.Generation, nil
}

// commandRandom 为确认令牌和命令 UUID 使用密码学随机源，失败时不降级为时间戳。
func commandRandom() (string, string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", "", errors.New("无法生成安全确认令牌，请稍后重试")
	}
	token := hex.EncodeToString(data[:16])
	id := data[16:]
	id[6], id[8] = (id[6]&0x0f)|0x40, (id[8]&0x3f)|0x80
	return token, fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

// PrepareCommand 从真实配置列表和最新快照生成六十秒单次确认，绝不在准备阶段发送控制。
func (s *Service) PrepareCommand(presetID string) (CommandConfirmation, error) {
	s.mu.Lock()
	p, adapter, parent, gen, err := s.commandContextLocked()
	if err != nil {
		s.mu.Unlock()
		return CommandConfirmation{}, err
	}
	s.state.PreparedCommand = nil
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	defer cancel()
	presets, err := adapter.ListPresets(ctx, "")
	if err != nil {
		return CommandConfirmation{}, err
	}
	var selected *autobgi.TaskPreset
	for _, preset := range presets {
		if preset.ID == presetID {
			copy := preset
			selected = &copy
			break
		}
	}
	if selected == nil {
		return CommandConfirmation{}, errors.New("所选配置不存在，请重新读取配置列表")
	}
	snapshot, ticket, err := s.readSnapshot(ctx, p, adapter, gen)
	if err != nil {
		return CommandConfirmation{}, err
	}
	if snapshot.ServerInstanceID == nil || snapshot.StateVersion == nil || snapshot.AccountID != nil || snapshot.Scope != "instance" || snapshot.Process.Running == nil {
		return CommandConfirmation{}, errors.New("实例身份或运行状态尚未核实，无法准备启动")
	}
	if *snapshot.Process.Running {
		return CommandConfirmation{}, errors.New("BGI 进程正在运行，请先在 AutoBGI 核对当前任务")
	}
	token, commandID, err := commandRandom()
	if err != nil {
		return CommandConfirmation{}, err
	}
	confirmation := CommandConfirmation{Token: token, ProfileID: p.ID, ProfileName: p.Name, BaseURL: p.BaseURL, InstanceID: *snapshot.ServerInstanceID, Scope: "instance", CommandID: commandID, Type: "oneLong.start", PresetID: selected.ID, PresetName: selected.Name, Impact: selected.Impact, ExpectedStateVersion: *snapshot.StateVersion, ExpiresAt: time.Now().Add(time.Minute).Format(time.RFC3339Nano)}
	s.mu.Lock()
	_, _, _, currentGen, contextErr := s.commandContextLocked()
	if contextErr != nil || ctx.Err() != nil || currentGen != gen || !s.currentLocked(gen, p.ID) || !s.canCommitSnapshotLocked(ticket, snapshot) || s.state.Capabilities.InstanceID == nil || *s.state.Capabilities.InstanceID != confirmation.InstanceID {
		s.mu.Unlock()
		return CommandConfirmation{}, errors.New("准备期间连接或实例已变化，请重新选择配置")
	}
	s.state.PreparedCommand = &confirmation
	s.preparedGeneration = gen
	snapshot.ProfileID = p.ID
	s.state.Snapshot = &snapshot
	s.mu.Unlock()
	s.publish()
	return confirmation, nil
}

// CancelPreparedCommand 只取消尚未提交的确认，不把关闭弹窗误认为服务端命令取消。
func (s *Service) CancelPreparedCommand(token string) AppState {
	s.mu.Lock()
	if s.state.PreparedCommand != nil && s.state.PreparedCommand.Token == token {
		s.state.PreparedCommand = nil
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState()
}

// ConfirmCommand 消费一次性令牌，复核状态并先记录发送意图；任何不确定结果都禁止自动重发。
func (s *Service) ConfirmCommand(token string) (autobgi.CommandReceipt, error) {
	s.mu.Lock()
	if s.state.PreparedCommand == nil || token == "" || s.state.PreparedCommand.Token != token {
		s.mu.Unlock()
		return autobgi.CommandReceipt{}, errors.New("确认已失效或已经使用，请重新准备命令")
	}
	frozen := *s.state.PreparedCommand
	s.state.PreparedCommand = nil
	p, adapter, parent, gen, err := s.commandContextLocked()
	expires, parseErr := time.Parse(time.RFC3339Nano, frozen.ExpiresAt)
	if err != nil || parseErr != nil || !time.Now().Before(expires) || gen != s.preparedGeneration || p.ID != frozen.ProfileID || p.BaseURL != frozen.BaseURL {
		s.mu.Unlock()
		s.publish()
		return autobgi.CommandReceipt{}, errors.New("确认已过期或连接已变化，请重新准备")
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	defer cancel()
	s.publish()
	snapshot, ticket, err := s.readSnapshot(ctx, p, adapter, gen)
	if err != nil {
		return autobgi.CommandReceipt{}, err
	}
	if snapshot.ServerInstanceID == nil || *snapshot.ServerInstanceID != frozen.InstanceID || snapshot.AccountID != nil || snapshot.Scope != frozen.Scope || snapshot.StateVersion == nil || *snapshot.StateVersion != frozen.ExpectedStateVersion || snapshot.Process.Running == nil || *snapshot.Process.Running {
		return autobgi.CommandReceipt{}, errors.New("实例运行状态已经变化，请重新选择配置并确认")
	}
	receipt := autobgi.CommandReceipt{CommandID: frozen.CommandID, Status: "sending", ProfileID: p.ID, InstanceID: frozen.InstanceID, PresetName: frozen.PresetName, Message: "发送意图已记录，正在提交；不会自动重发", UpdatedAt: time.Now().Format(time.RFC3339Nano)}
	s.mu.Lock()
	_, _, _, currentGen, contextErr := s.commandContextLocked()
	// 门等待或网络读取也会消耗确认有效期，记录发送意图前必须再次核对截止时间。
	if contextErr != nil || ctx.Err() != nil || !time.Now().Before(expires) || currentGen != gen || !s.currentLocked(gen, p.ID) || !s.canCommitSnapshotLocked(ticket, snapshot) {
		s.mu.Unlock()
		return autobgi.CommandReceipt{}, errors.New("连接已变化，命令未发送")
	}
	next := cloneJournal(s.journal)
	err = putCommand(&next, p.BaseURL, receipt)
	if err == nil {
		err = s.commitJournalLocked(next)
	}
	s.mu.Unlock()
	if err != nil {
		s.publish()
		return autobgi.CommandReceipt{}, err
	}
	s.publish()
	request := autobgi.CommandRequest{CommandID: frozen.CommandID, Type: frozen.Type, Params: autobgi.CommandParams{PresetID: frozen.PresetID}, ExpectedStateVersion: frozen.ExpectedStateVersion}
	response, sendErr := adapter.ExecuteCommand(ctx, request)
	if sendErr == nil {
		response.ProfileID, response.InstanceID, response.PresetName = p.ID, frozen.InstanceID, frozen.PresetName
		receipt = response
	} else {
		receipt.Status = "unknown"
		receipt.Message = "请求结果未知，请查询此命令回执并核对 AutoBGI；不会自动重发"
		var apiErr *autobgi.APIError
		if errors.As(sendErr, &apiErr) {
			switch apiErr.Code {
			case "TASK_BUSY", "VERSION_CONFLICT", "INVALID_ARGUMENT", "IDEMPOTENCY_CONFLICT", "UNSUPPORTED_CAPABILITY", "AUTH_INVALID", "FORBIDDEN":
				receipt.Status, receipt.Message = "rejected", apiErr.Message
			}
		}
	}
	receipt.UpdatedAt = time.Now().Format(time.RFC3339Nano)
	s.mu.Lock()
	next = cloneJournal(s.journal)
	err = putCommand(&next, p.BaseURL, receipt)
	var notice *Notification
	if err == nil {
		notice, err = s.stageCommandNotificationLocked(&next, p, receipt, receipt.UpdatedAt, true)
	}
	if err == nil {
		err = s.commitJournalLocked(next)
	}
	if err == nil && notice != nil {
		s.enqueueCommittedNoticesLocked(*notice)
	}
	// 返回已提交的最新回执，防止迟到 POST 覆盖事件确认的更高阶段结果。
	for _, entry := range s.journal.Commands {
		if entry.Receipt.CommandID == receipt.CommandID && entry.Receipt.ProfileID == p.ID {
			receipt = entry.Receipt
		}
	}
	s.mu.Unlock()
	s.publish()
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}

// GetCommand 只查询当前来源已记录的命令，不提供任意 UUID 或旧实例的跨上下文入口。
func (s *Service) GetCommand(commandID string) (autobgi.CommandReceipt, error) {
	s.mu.Lock()
	p, ok := s.activeProfileLocked()
	if !ok || s.closed || s.paused || s.adapter == nil || s.connectionCtx == nil || s.state.Connection.Status != "online" || s.state.Capabilities == nil || !s.state.Capabilities.CommandStatus || s.state.Capabilities.InstanceID == nil {
		s.mu.Unlock()
		return autobgi.CommandReceipt{}, errors.New("请先连接提供命令回执查询的当前实例")
	}
	var known *autobgi.CommandReceipt
	for _, entry := range s.journal.Commands {
		if entry.Receipt.CommandID == commandID && entry.Receipt.ProfileID == p.ID && entry.BaseURL == p.BaseURL && entry.Receipt.InstanceID == *s.state.Capabilities.InstanceID {
			copy := entry.Receipt
			known = &copy
			break
		}
	}
	if known == nil {
		s.mu.Unlock()
		return autobgi.CommandReceipt{}, errors.New("当前实例没有此命令的本地记录")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(10 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return *known, err
	}
	s.mu.Unlock()
	defer release()
	receipt, readErr := ticket.adapter.GetCommand(ctx, commandID)
	s.mu.Lock()
	if !s.manualReadCurrentLocked(ticket, ctx) {
		s.mu.Unlock()
		return *known, errors.New("查询期间连接、能力或暂停状态已变化，此次回执已丢弃")
	}
	if readErr != nil {
		s.mu.Unlock()
		return *known, readErr
	}
	receipt.ProfileID, receipt.InstanceID, receipt.PresetName = p.ID, known.InstanceID, known.PresetName
	next := cloneJournal(s.journal)
	err = putCommand(&next, p.BaseURL, receipt)
	var notice *Notification
	if err == nil {
		notice, err = s.stageCommandNotificationLocked(&next, p, receipt, receipt.UpdatedAt, true)
	}
	if err == nil {
		err = s.commitJournalLocked(next)
	}
	if err == nil && notice != nil {
		s.enqueueCommittedNoticesLocked(*notice)
	}
	// 返回已提交的最高阶段，查询期间到达的事件终态不能被迟到 accepted 回包降级。
	if err == nil {
		for _, entry := range s.journal.Commands {
			if entry.Receipt.CommandID == commandID && entry.Receipt.ProfileID == p.ID && entry.BaseURL == p.BaseURL && entry.Receipt.InstanceID == known.InstanceID {
				receipt = entry.Receipt
				break
			}
		}
	}
	s.mu.Unlock()
	s.publish()
	return receipt, err
}
