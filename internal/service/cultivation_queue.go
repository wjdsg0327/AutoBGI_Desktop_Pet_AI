package service

import (
	"context"
	"errors"
	"time"

	"autobgi-pet/internal/autobgi"
)

// CultivationQueueSummary 将只读结果绑定到实际连接和明确预设，未知源时间仍保留在结果内部。
type CultivationQueueSummary struct {
	ProfileID  string                         `json:"profileId"`
	Generation uint64                         `json:"generation"`
	InstanceID string                         `json:"instanceId"`
	PresetID   string                         `json:"presetId"`
	FetchedAt  string                         `json:"fetchedAt"`
	Stale      bool                           `json:"stale"`
	Result     *CultivationQueueDisplayResult `json:"result"`
}

// ReadCultivationQueue 共用唯一连接且不依赖查询角色选择，不修改配置、默认账号或执行身份。
func (s *Service) ReadCultivationQueue(presetID string) (CultivationQueueSummary, error) {
	if !autobgi.ValidCultivationQueuePreset(presetID) {
		return CultivationQueueSummary{}, &autobgi.APIError{Code: "INVALID_ARGUMENT", Message: "请选择有效的一条龙预设"}
	}
	s.mu.Lock()
	if s.closed || s.paused || s.state.Connection.Status != "online" || s.adapter == nil || s.client == nil ||
		s.connectionCtx == nil || s.connectionCtx.Err() != nil || s.state.Capabilities == nil ||
		s.state.Capabilities.InstanceID == nil || !s.state.Capabilities.CultivationQueue ||
		s.state.Capabilities.SourceMode != "pet-v1" || s.state.Capabilities.AccountScope != "instance" {
		s.mu.Unlock()
		return CultivationQueueSummary{}, errors.New("请先连接支持培养队列只读查询的当前实例")
	}
	reader, supported := s.adapter.(autobgi.CultivationQueueReader)
	p, valid := s.activeProfileLocked()
	if !supported || !valid || !validID(p.ID) || p.CredentialRef != "AutoBGI-Pet/profile/"+p.ID {
		s.mu.Unlock()
		return CultivationQueueSummary{}, errors.New("当前连接资料或培养队列读取能力已失效")
	}
	if normalized, err := autobgi.NormalizeURL(p.BaseURL, p.AllowInsecure); err != nil || normalized != p.BaseURL {
		s.mu.Unlock()
		return CultivationQueueSummary{}, errors.New("当前连接地址已失效，请重新保存并连接")
	}
	// 复用现有暂停周期取消源，休眠后快速恢复也不能接纳暂停前的迟到结果。
	if s.snapshotContext == nil {
		s.snapshotContext, s.snapshotCancel = context.WithCancel(s.connectionCtx)
	}
	ctx, cancel := context.WithTimeout(s.snapshotContext, 20*time.Second)
	client := s.client
	generation, epoch := s.state.Connection.Generation, s.snapshotEpoch
	instanceID := *s.state.Capabilities.InstanceID
	expectedSchema := autobgi.CultivationQueueSchemaVersion(*s.state.Capabilities)
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	defer cancel()

	result, readErr := reader.ReadCultivationQueue(ctx, presetID)
	if readErr == nil {
		// 模拟读取器同样经过完整合同和密钥脱敏，不能绕过适配器的身份与未知库存校验。
		result, readErr = client.RedactCultivationQueueSchema(result, instanceID, presetID, expectedSchema)
	}
	s.mu.Lock()
	currentProfile, profileValid := s.activeProfileLocked()
	current := s.currentLocked(generation, p.ID) && !s.paused && s.snapshotEpoch == epoch &&
		s.state.Connection.Status == "online" && s.client == client && profileValid && currentProfile == p &&
		s.state.Capabilities != nil && s.state.Capabilities.CultivationQueue &&
		autobgi.CultivationQueueSchemaVersion(*s.state.Capabilities) == expectedSchema &&
		s.state.Capabilities.SourceMode == "pet-v1" && s.state.Capabilities.AccountScope == "instance" &&
		s.state.Capabilities.InstanceID != nil && *s.state.Capabilities.InstanceID == instanceID && ctx.Err() == nil
	s.mu.Unlock()
	if !current {
		return CultivationQueueSummary{}, errors.New("培养队列读取期间连接、实例或暂停状态已变化，请重新读取")
	}
	if readErr != nil {
		// 不转发模拟读取器或其他实现的异常正文，防止密钥反射到错误提示。
		if apiErr, ok := readErr.(*autobgi.APIError); ok {
			messages := map[string]string{
				"AUTH_INVALID":           "当前实例鉴权失败，请重新连接",
				"UNSUPPORTED_CAPABILITY": "当前服务未提供培养队列只读查询",
				"NOT_FOUND":              "当前服务未提供培养队列接口或所选预设已不存在",
				"PRESET_NOT_FOUND":       "所选一条龙预设已不存在，请重新读取列表",
				"CONTEXT_CHANGED":        "培养队列执行目录或预设已变化，请重新读取",
				"INVALID_ARGUMENT":       "所选培养队列预设无效",
				"INVALID_RESPONSE":       "培养队列响应未通过身份或数据校验",
				"INCOMPATIBLE_RESPONSE":  "培养队列响应结构不兼容",
				"DATA_UNAVAILABLE":       "培养队列缓存暂不可用",
				"NETWORK_ERROR":          "培养队列读取失败，请检查连接",
				"RATE_LIMITED":           "培养队列请求过于频繁，请稍后重试",
			}
			if message, known := messages[apiErr.Code]; known {
				return CultivationQueueSummary{}, &autobgi.APIError{Code: apiErr.Code, Message: message, HTTPStatus: apiErr.HTTPStatus}
			}
		}
		return CultivationQueueSummary{}, errors.New("培养队列读取失败，请检查当前实例后重试")
	}
	return CultivationQueueSummary{
		ProfileID: p.ID, Generation: generation, InstanceID: instanceID, PresetID: presetID,
		FetchedAt: time.Now().UTC().Format(time.RFC3339Nano), Result: cultivationQueueDisplay(result),
	}, nil
}
